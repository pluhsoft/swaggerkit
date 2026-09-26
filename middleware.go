package swaggerkit

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// CORSOptions configures [CORS].
type CORSOptions struct {
	// AllowedOrigins lists origins such as "https://app.example.com".
	// "*" allows any origin and cannot be combined with AllowCredentials.
	AllowedOrigins []string
	// AllowedMethods defaults to GET, HEAD, POST, PUT, PATCH, DELETE.
	AllowedMethods []string
	// AllowedHeaders defaults to Content-Type and Authorization.
	AllowedHeaders []string
	// ExposedHeaders are response headers readable by the browser.
	ExposedHeaders []string
	// AllowCredentials allows cookies and HTTP authentication.
	AllowCredentials bool
	// MaxAge is how long browsers cache preflight results. Defaults to 10 minutes.
	MaxAge time.Duration
}

// CORS returns a middleware that implements Cross-Origin Resource Sharing.
// Add it with [API.Use] so that preflight requests reach it.
func CORS(opts CORSOptions) Middleware {
	anyOrigin := slices.Contains(opts.AllowedOrigins, "*")
	if anyOrigin && opts.AllowCredentials {
		panic("swaggerkit: CORS: AllowedOrigins \"*\" cannot be used with AllowCredentials")
	}
	methods := opts.AllowedMethods
	if len(methods) == 0 {
		methods = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	}
	headers := opts.AllowedHeaders
	if len(headers) == 0 {
		headers = []string{"Content-Type", "Authorization"}
	}
	allowedHeader := map[string]bool{}
	for _, h := range headers {
		allowedHeader[strings.ToLower(h)] = true
	}
	maxAge := opts.MaxAge
	if maxAge == 0 {
		maxAge = 10 * time.Minute
	}
	originAllowed := func(origin string) bool {
		return anyOrigin || slices.Contains(opts.AllowedOrigins, origin)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			h := w.Header()
			h.Add("Vary", "Origin")
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			if origin == "" || !originAllowed(origin) {
				if preflight {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if anyOrigin {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			if opts.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if !preflight {
				if len(opts.ExposedHeaders) > 0 {
					h.Set("Access-Control-Expose-Headers", strings.Join(opts.ExposedHeaders, ", "))
				}
				next.ServeHTTP(w, r)
				return
			}

			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			if !slices.Contains(methods, r.Header.Get("Access-Control-Request-Method")) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			for _, name := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				if name = strings.TrimSpace(name); name != "" && !allowedHeader[strings.ToLower(name)] {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
			h.Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))
			h.Set("Access-Control-Allow-Headers", strings.Join(headers, ", "))
			h.Set("Access-Control-Max-Age", strconv.Itoa(int(maxAge.Seconds())))
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

// RequestLogger returns a middleware that logs every request with its
// method, path, status, size and duration. Records use the request context.
func RequestLogger(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			ctx := r.Context()
			extra, _ := ctx.Value(logAttrsKey).([]slog.Attr)
			logger.LogAttrs(ctx, level, "http request", append(extra,
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
			)...)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status, s.wroteHeader = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
