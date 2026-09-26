package swaggerkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestAPI(opts ...Option) (*API, *bytes.Buffer) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	api := New(Info{Title: "Test", Version: "1.0.0"}, append([]Option{WithLogger(logger)}, opts...)...)
	return api, &logs
}

type request struct {
	method, target, body string
	header               map[string]string
}

func do(t *testing.T, h http.Handler, r request) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req := httptest.NewRequest(r.method, r.target, body)
	if r.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int, bodyContains ...string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d; body: %s", rec.Code, status, rec.Body)
	}
	for _, s := range bodyContains {
		if !strings.Contains(rec.Body.String(), s) {
			t.Fatalf("body %q does not contain %q", rec.Body, s)
		}
	}
}

type paramsInput struct {
	ID      int64      `path:"id" validate:"min=1"`
	Limit   int        `query:"limit" default:"10" validate:"max=50"`
	Tags    []string   `query:"tag"`
	Flag    bool       `query:"flag"`
	Since   *time.Time `query:"since"`
	Kind    color      `query:"kind"`
	Trace   string     `header:"X-Trace-Id" validate:"required"`
	Session string     `cookie:"session"`
	Lang    []string   `header:"Accept-Language"`
}

func TestParams(t *testing.T) {
	api, _ := newTestAPI()
	Get(api, "/hives/{id}", func(ctx context.Context, in paramsInput) (paramsInput, error) { return in, nil })

	h := map[string]string{"X-Trace-Id": "t1", "Cookie": "session=abc"}
	rec := do(t, api, request{method: "GET", target: "/hives/7?tag=a&tag=b&flag=1&since=2026-09-26T10:00:00Z&kind=red", header: h})
	expect(t, rec, 200)
	var got paramsInput
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Limit != 10 || len(got.Tags) != 2 || !got.Flag || got.Since == nil || got.Kind != "red" || got.Trace != "t1" || got.Session != "abc" {
		t.Fatalf("bound %+v", got)
	}

	tests := []struct {
		target string
		header map[string]string
		want   string
	}{
		{"/hives/0", h, `"location":"path.id","message":"must be greater than or equal to 1"`},
		{"/hives/x", h, `"location":"path.id","message":"must be an integer"`},
		{"/hives/1?limit=99", h, `"location":"query.limit"`},
		{"/hives/1?limit=1&limit=2", h, `"message":"must be given once"`},
		{"/hives/1?flag=yes", h, `"message":"must be true or false"`},
		{"/hives/1?since=yesterday", h, `"location":"query.since"`},
		{"/hives/1?kind=blue", h, `must be one of`},
		{"/hives/1", nil, `"location":"header.X-Trace-Id","message":"is required"`},
	}
	for _, tt := range tests {
		expect(t, do(t, api, request{method: "GET", target: tt.target, header: tt.header}), 422, tt.want)
	}
	// An empty value is treated as missing, so the default applies.
	expect(t, do(t, api, request{method: "GET", target: "/hives/1?limit=", header: h}), 200, `"Limit":10`)
}

type hive struct {
	Name   string   `json:"name" validate:"min=1"`
	Bees   int      `json:"bees,omitempty" default:"100"`
	Status string   `json:"status,omitempty" validate:"oneof=active empty"`
	Tags   []string `json:"tags,omitempty"`
}

func TestBody(t *testing.T) {
	api, _ := newTestAPI(WithMaxBodyBytes(64))
	Post(api, "/hives", func(ctx context.Context, in hive) (hive, error) { return in, nil })
	type optionalBody struct {
		Body *hive
	}
	Put(api, "/optional", func(ctx context.Context, in optionalBody) (bool, error) { return in.Body == nil, nil })

	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":"Linden"}`}), 200, `"bees":100`)
	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":""}`}), 422, `"body.name"`)
	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":"a","extra":1}`}), 422, `"body.extra","message":"unknown field"`)
	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":"a"} {}`}), 400, "single JSON value")
	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":`}), 400, "not valid JSON")
	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":"` + strings.Repeat("a", 100) + `"}`}), 413)
	expect(t, do(t, api, request{method: "POST", target: "/hives"}), 422, `"location":"body","message":"is required"`)
	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":"a"}`,
		header: map[string]string{"Content-Type": "text/plain"}}), 415)
	expect(t, do(t, api, request{method: "POST", target: "/hives", body: `{"name":"a"}`,
		header: map[string]string{"Content-Type": "application/merge-patch+json; charset=utf-8"}}), 200)
	expect(t, do(t, api, request{method: "PUT", target: "/optional"}), 200, "true")
	expect(t, do(t, api, request{method: "PUT", target: "/optional", body: `{"name":"a"}`}), 200, "false")

	lenient, _ := newTestAPI(WithUnknownFields())
	Post(lenient, "/hives", func(ctx context.Context, in hive) (hive, error) { return in, nil })
	expect(t, do(t, lenient, request{method: "POST", target: "/hives", body: `{"name":"a","extra":1}`}), 200)
}

func TestOutputs(t *testing.T) {
	api, logs := newTestAPI()
	type none struct{}
	Delete(api, "/no-content", func(ctx context.Context, _ none) (NoContent, error) { return NoContent{}, nil })
	Get(api, "/nil-pointer", func(ctx context.Context, _ none) (*hive, error) { return nil, nil })
	Get(api, "/nil-slice", func(ctx context.Context, _ none) ([]hive, error) { return nil, nil })
	Post(api, "/created", func(ctx context.Context, _ none) (hive, error) { return hive{Name: "a"}, nil }, Status(201))
	Get(api, "/file", func(ctx context.Context, _ none) (*File, error) {
		return &File{Body: strings.NewReader("honeycomb"), ContentType: "text/plain", Name: "comb.txt"}, nil
	})
	Get(api, "/stream", func(ctx context.Context, _ none) (*File, error) {
		return &File{Body: io.NopCloser(strings.NewReader("stream")), Inline: true}, nil
	})
	Get(api, "/nil-file", func(ctx context.Context, _ none) (*File, error) { return nil, nil })
	Get(api, "/headers", func(ctx context.Context, _ none) (string, error) {
		ResponseHeader(ctx).Set("X-Hive", "1")
		return Request(ctx).URL.Path, nil
	})

	expect(t, do(t, api, request{method: "DELETE", target: "/no-content"}), 204)
	expect(t, do(t, api, request{method: "GET", target: "/nil-pointer"}), 500)
	if !strings.Contains(logs.String(), "returned a nil *swaggerkit.hive") {
		t.Errorf("nil output not logged: %s", logs)
	}
	expect(t, do(t, api, request{method: "GET", target: "/nil-slice"}), 200, "[]")
	expect(t, do(t, api, request{method: "POST", target: "/created"}), 201)

	rec := do(t, api, request{method: "GET", target: "/file", header: map[string]string{"Range": "bytes=0-4"}})
	expect(t, rec, 206, "honey")
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename=comb.txt` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	rec = do(t, api, request{method: "GET", target: "/stream"})
	expect(t, rec, 200, "stream")
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	expect(t, do(t, api, request{method: "GET", target: "/nil-file"}), 500)

	rec = do(t, api, request{method: "GET", target: "/headers"})
	expect(t, rec, 200, `"/headers"`)
	if rec.Header().Get("X-Hive") != "1" || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", rec.Header())
	}
	rec = do(t, api, request{method: "HEAD", target: "/headers"})
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d %q", rec.Code, rec.Body)
	}
}

func TestErrors(t *testing.T) {
	api, logs := newTestAPI()
	type none struct{}
	Get(api, "/not-found", func(ctx context.Context, _ none) (hive, error) { return hive{}, NotFound("no hive") })
	Get(api, "/wrapped", func(ctx context.Context, _ none) (hive, error) {
		return hive{}, errorsJoin(Conflict("busy"))
	})
	Get(api, "/internal", func(ctx context.Context, _ none) (hive, error) {
		return hive{}, errors.New("database password is hunter2")
	})
	Get(api, "/panic", func(ctx context.Context, _ none) (hive, error) { panic("queen escaped") })
	Get(api, "/abort", func(ctx context.Context, _ none) (hive, error) { panic(http.ErrAbortHandler) })

	rec := do(t, api, request{method: "GET", target: "/not-found"})
	expect(t, rec, 404, `{"type":"about:blank","title":"Not Found","status":404,"detail":"no hive"}`)
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	expect(t, do(t, api, request{method: "GET", target: "/wrapped"}), 409, "busy")
	rec = do(t, api, request{method: "GET", target: "/internal"})
	expect(t, rec, 500, `"title":"Internal Server Error"`)
	if strings.Contains(rec.Body.String(), "hunter2") || !strings.Contains(logs.String(), "hunter2") {
		t.Errorf("internal error must be logged, not sent: body %s", rec.Body)
	}
	expect(t, do(t, api, request{method: "GET", target: "/panic"}), 500)
	if !strings.Contains(logs.String(), "queen escaped") {
		t.Error("panic not logged")
	}
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Error("ErrAbortHandler must propagate")
			}
		}()
		do(t, api, request{method: "GET", target: "/abort"})
	}()

	e := Errorf(http.StatusBadGateway, "upstream: %w", io.EOF)
	if !errors.Is(e, io.EOF) || e.Error() != "502 Bad Gateway: upstream: EOF: EOF" {
		t.Errorf("Errorf = %q", e.Error())
	}
	if got := (&Error{Status: 999}).problem().Status; got != 500 {
		t.Errorf("invalid status became %d", got)
	}
}

func errorsJoin(err error) error { return errors.Join(errors.New("context"), err) }

func TestMiddlewaresAndGroups(t *testing.T) {
	api, _ := newTestAPI()
	var order []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	api.Use(mw("api"))
	g := api.Group("/v2/", Tags("g"), Middlewares(mw("group")))
	type none struct{}
	Get(g.Group("hives"), "/", func(ctx context.Context, _ none) (string, error) {
		order = append(order, "handler")
		return "ok", nil
	}, Middlewares(mw("route")))
	Get(api, "/", func(ctx context.Context, _ none) (string, error) { return "root", nil })

	expect(t, do(t, api, request{method: "GET", target: "/v2/hives"}), 200)
	if got := strings.Join(order, ","); got != "api,group,route,handler" {
		t.Errorf("order = %s", got)
	}
	expect(t, do(t, api, request{method: "GET", target: "/"}), 200, "root")
	expect(t, do(t, api, request{method: "GET", target: "/unknown"}), 404)
	expect(t, do(t, api, request{method: "POST", target: "/"}), 405)
}

func TestBasePath(t *testing.T) {
	api, _ := newTestAPI(WithBasePath("/api/v1/"), WithServer("https://hive.example/", "prod"), WithDocs("docs"))
	type none struct{}
	Get(api, "/hives", func(ctx context.Context, _ none) (string, error) { return "ok", nil })
	expect(t, do(t, api, request{method: "GET", target: "/api/v1/hives"}), 200)
	expect(t, do(t, api, request{method: "GET", target: "/hives"}), 404)
	expect(t, do(t, api, request{method: "GET", target: "/api/v1/docs"}), 200, `"/api/v1/docs/openapi.json"`)
	expect(t, do(t, api, request{method: "GET", target: "/api/v1/docs/openapi.json"}), 200,
		`"url": "https://hive.example/api/v1"`, `"/hives"`)

	root, _ := newTestAPI(WithDocs("/"))
	expect(t, do(t, root, request{method: "GET", target: "/"}), 200, "swagger-ui")
	expect(t, do(t, root, request{method: "GET", target: "/openapi.json"}), 200, `"openapi": "3.1.0"`)
}

func TestRegistrationPanics(t *testing.T) {
	type none struct{}
	type idInput struct {
		ID int `path:"id"`
	}
	type mixed struct {
		ID   int `path:"id"`
		Name string
	}
	type badParam struct {
		ID  int            `path:"id"`
		Map map[string]int `query:"map"`
	}
	type sliceInPath struct {
		IDs []int `path:"ids"`
	}
	handler := func(ctx context.Context, _ none) (string, error) { return "", nil }
	tests := []struct {
		name string
		reg  func(a *API)
		want string
	}{
		{"missing field", func(a *API) { Get(a, "/hives/{id}", handler) }, "path parameter {id} has no field"},
		{"extra field", func(a *API) {
			Get(a, "/hives", func(ctx context.Context, _ idInput) (string, error) { return "", nil })
		}, "the path has no {id}"},
		{"regexp path", func(a *API) { Get(a, "/hives/{id:[0-9]+}", handler) }, "regular expressions are not supported"},
		{"double slash", func(a *API) { Get(a, "/a//b", handler) }, "must not contain //"},
		{"wildcard not last", func(a *API) { Get(a, "/{rest...}/x", handler) }, "must be the last segment"},
		{"get body", func(a *API) { Get(a, "/hives", func(ctx context.Context, _ hive) (string, error) { return "", nil }) }, "cannot have a body"},
		{"mixed input", func(a *API) {
			Get(a, "/hives/{id}", func(ctx context.Context, _ mixed) (string, error) { return "", nil })
		}, "field Name needs a path, query, header or cookie tag"},
		{"bad param type", func(a *API) {
			Get(a, "/hives/{id}", func(ctx context.Context, _ badParam) (string, error) { return "", nil })
		}, "parameter type map[string]int is not supported"},
		{"slice in path", func(a *API) {
			Get(a, "/hives/{ids}", func(ctx context.Context, _ sliceInPath) (string, error) { return "", nil })
		}, "path parameters cannot be slices"},
		{"duplicate operation", func(a *API) {
			Get(a, "/a", handler, OperationID("x"))
			Get(a, "/b", handler, OperationID("x"))
		}, `operation ID "x" is already used`},
		{"unknown scheme", func(a *API) { Get(a, "/a", handler, Security("jwt")) }, `unknown security scheme "jwt"`},
		{"bad status", func(a *API) { Get(a, "/a", handler, Status(404)) }, "must be 1xx-3xx"},
		{"bad method", func(a *API) { Handle(a, "BREW", "/a", handler) }, "unsupported method"},
		{"file by value", func(a *API) { Get(a, "/a", func(ctx context.Context, _ none) (File, error) { return File{}, nil }) }, "use *swaggerkit.File"},
		{"nil handler", func(a *API) { Get[none, string](a, "/a", nil) }, "nil handler"},
		{"conflict", func(a *API) {
			Get(a, "/a", handler, OperationID("a1"))
			Get(a, "/a", handler, OperationID("a2"))
		}, "conflicts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("no panic")
				}
				if msg := panicText(r); !strings.Contains(msg, tt.want) {
					t.Fatalf("panic %q, want %q", msg, tt.want)
				}
			}()
			api, _ := newTestAPI()
			tt.reg(api)
		})
	}
}

func panicText(r any) string {
	if err, ok := r.(error); ok {
		return err.Error()
	}
	s, _ := r.(string)
	return s
}

func (a *API) handlerName(t *testing.T, path string) string {
	t.Helper()
	for _, rt := range a.routes {
		if rt.path == path {
			return rt.operationID + "|" + rt.summary
		}
	}
	t.Fatalf("no route %s", path)
	return ""
}

type hiveService struct{}

func (hiveService) GetHiveByID(ctx context.Context, _ struct{}) (string, error) { return "", nil }

func ListHives(ctx context.Context, _ struct{}) (string, error) { return "", nil }

func TestNaming(t *testing.T) {
	api, _ := newTestAPI()
	Get(api, "/a", ListHives)
	Get(api, "/b", hiveService{}.GetHiveByID)
	Get(api, "/c/{id}/honey", func(ctx context.Context, _ struct {
		ID int `path:"id"`
	}) (string, error) {
		return "", nil
	})
	for path, want := range map[string]string{
		"/a":            "listHives|List hives",
		"/b":            "getHiveByID|Get hive by ID",
		"/c/{id}/honey": "getCIdHoney|",
	} {
		if got := api.handlerName(t, path); got != want {
			t.Errorf("%s: %s, want %s", path, got, want)
		}
	}
	if got := splitWords("HTTPServerURL2Go"); strings.Join(got, " ") != "HTTP Server URL2 Go" {
		t.Errorf("splitWords = %v", got)
	}
}

func TestScalarParams(t *testing.T) {
	api, _ := newTestAPI()
	type in struct {
		Count *int      `query:"count"`
		IDs   []uint16  `query:"id"`
		Ratio float32   `query:"ratio"`
		Flags []*bool   `query:"flag"`
		Name  *string   `query:"name"`
		When  time.Time `query:"when"`
	}
	Get(api, "/p", func(ctx context.Context, in in) (in, error) { return in, nil })
	rec := do(t, api, request{method: "GET", target: "/p?count=3&id=1&id=255&ratio=0.5&flag=true&flag=0&name=bee&when=2026-09-26T10:00:00Z"})
	expect(t, rec, 200, `"Count":3`, `"IDs":[1,255]`, `"Ratio":0.5`, `"Flags":[true,false]`, `"Name":"bee"`, `"When":"2026-09-26T10:00:00Z"`)
	expect(t, do(t, api, request{method: "GET", target: "/p?id=65536"}), 422, `"location":"query.id[0]"`)
}
