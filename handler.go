package swaggerkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"reflect"
	"runtime/debug"
	"time"
)

// NoContent is an output type for handlers without a response body.
// The route responds with 204 No Content.
type NoContent struct{}

// File is an output type for handlers that return a file or any other
// non-JSON body. Use *File as the output type.
type File struct {
	Body        io.Reader // if it is an io.ReadSeeker, range requests are supported
	ContentType string    // defaults to "application/octet-stream"
	Name        string    // file name for Content-Disposition
	Inline      bool      // show in the browser instead of downloading
	ModTime     time.Time // enables If-Modified-Since for io.ReadSeeker bodies
}

type outputKind int

const (
	outputJSON outputKind = iota
	outputNoContent
	outputFile
)

var (
	noContentType = reflect.TypeFor[NoContent]()
	fileType      = reflect.TypeFor[File]()
)

func analyzeOutput(g *schemaGen, t reflect.Type, where string) (outputKind, *Schema) {
	switch {
	case t == noContentType:
		return outputNoContent, nil
	case t == reflect.PointerTo(fileType):
		return outputFile, nil
	case deref(t) == fileType:
		fail("output", "use *swaggerkit.File as the output type")
	}
	return outputJSON, g.schemaOf(t, "output")
}

type contextKey int

const (
	requestKey contextKey = iota
	writerKey
)

// Request returns the HTTP request of a handler context.
func Request(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey).(*http.Request)
	return r
}

// ResponseHeader returns the response headers of a handler context.
// Set headers before the handler returns.
func ResponseHeader(ctx context.Context) http.Header {
	if w, ok := ctx.Value(writerKey).(http.ResponseWriter); ok {
		return w.Header()
	}
	return http.Header{}
}

// logError reports a failure that the client only sees as 500:
// a handler error, a panic or a response that cannot be encoded.
func (a *API) logError(ctx context.Context, msg string, attrs ...slog.Attr) {
	if a.logger != nil {
		a.logger.LogAttrs(ctx, slog.LevelError, msg, attrs...)
	}
}

// serve runs a route: input binding, the handler and the response.
func serve[In, Out any](a *API, rt *route, w http.ResponseWriter, r *http.Request, h HandlerFunc[In, Out]) {
	ctx := context.WithValue(r.Context(), requestKey, r)
	ctx = context.WithValue(ctx, writerKey, w)
	defer func() {
		if rec := recover(); rec != nil {
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			a.logError(ctx, "swaggerkit: handler panicked",
				slog.String("operation", rt.operationID),
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())))
			a.writeError(ctx, w, &Error{Status: http.StatusInternalServerError, Err: fmt.Errorf("panic: %v", rec)})
		}
	}()

	limit := a.maxBodyBytes
	if rt.cfg.maxBodyBytes > 0 {
		limit = rt.cfg.maxBodyBytes
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll() // temporary files of uploads
		}
	}()
	in, verr := rt.input.bind(a, w, r, limit)
	if verr != nil {
		a.writeError(ctx, w, verr)
		return
	}
	out, err := h(ctx, in.Interface().(In))
	if err != nil {
		a.writeError(ctx, w, err)
		return
	}
	a.writeOutput(ctx, w, r, rt, out)
}

func (a *API) writeOutput(ctx context.Context, w http.ResponseWriter, r *http.Request, rt *route, out any) {
	switch rt.output {
	case outputNoContent:
		w.WriteHeader(successStatus(rt))
		return
	case outputFile:
		f, _ := out.(*File)
		if f == nil || f.Body == nil {
			a.writeError(ctx, w, &Error{Status: http.StatusInternalServerError, Err: errors.New("handler returned a nil *File")})
			return
		}
		writeFile(w, r, f, successStatus(rt))
		return
	}

	v := reflect.ValueOf(&out).Elem().Elem()
	if !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil()) || (v.Kind() == reflect.Map && v.IsNil()) {
		a.writeError(ctx, w, &Error{Status: http.StatusInternalServerError,
			Err: fmt.Errorf("handler %s returned a nil %T without an error", rt.operationID, out)})
		return
	}
	if v.Kind() == reflect.Slice && v.IsNil() {
		out = []struct{}{} // encode a nil slice as [] rather than null
	}
	body, err := marshalJSON(out)
	if err != nil {
		a.writeError(ctx, w, &Error{Status: http.StatusInternalServerError, Err: fmt.Errorf("encode response: %w", err)})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(successStatus(rt))
	if r.Method != http.MethodHead {
		_, _ = w.Write(append(body, '\n'))
	}
}

func successStatus(rt *route) int {
	switch {
	case rt.cfg.status != 0:
		return rt.cfg.status
	case rt.output == outputNoContent:
		return http.StatusNoContent
	}
	return http.StatusOK
}

func writeFile(w http.ResponseWriter, r *http.Request, f *File, status int) {
	if c, ok := f.Body.(io.Closer); ok {
		defer c.Close()
	}
	h := w.Header()
	ct := f.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	if f.Name != "" || !f.Inline {
		disposition := "attachment"
		if f.Inline {
			disposition = "inline"
		}
		params := map[string]string{}
		if f.Name != "" {
			params["filename"] = f.Name
		}
		h.Set("Content-Disposition", mime.FormatMediaType(disposition, params))
	}
	if rs, ok := f.Body.(io.ReadSeeker); ok && status == http.StatusOK {
		http.ServeContent(w, r, "", f.ModTime, rs)
		return
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f.Body)
	}
}

// writeError sends err as problem details. Errors that are not *Error become
// 500 without details; 5xx errors are logged with their cause.
func (a *API) writeError(ctx context.Context, w http.ResponseWriter, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Status: http.StatusInternalServerError, Err: err}
	}
	p := e.problem()
	if p.Status >= 500 {
		attrs := []slog.Attr{slog.Int("status", p.Status)}
		if r := Request(ctx); r != nil {
			attrs = append(attrs, slog.String("method", r.Method), slog.String("path", r.URL.Path))
		}
		attrs = append(attrs, slog.Any("error", err))
		a.logError(ctx, "swaggerkit: request failed", attrs...)
	}
	body, merr := marshalJSON(p)
	if merr != nil {
		body = []byte(`{"type":"about:blank","title":"Internal Server Error","status":500}`)
		p.Status = http.StatusInternalServerError
	}
	h := w.Header()
	h.Set("Content-Type", "application/problem+json")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Del("Content-Disposition")
	w.WriteHeader(p.Status)
	_, _ = w.Write(append(body, '\n'))
}
