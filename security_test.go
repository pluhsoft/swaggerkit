package swaggerkit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecuritySchemeDocuments(t *testing.T) {
	tests := []struct {
		scheme SecurityScheme
		want   string
	}{
		{BearerAuth(""), `{"type":"http","scheme":"bearer"}`},
		{BearerAuth("JWT"), `{"type":"http","scheme":"bearer","bearerFormat":"JWT"}`},
		{BasicAuth(), `{"type":"http","scheme":"basic"}`},
		{APIKeyAuth("cookie", "sid"), `{"type":"apiKey","in":"cookie","name":"sid"}`},
		{OpenIDConnectAuth("https://id.example"), `{"type":"openIdConnect","openIdConnectUrl":"https://id.example"}`},
	}
	for _, tt := range tests {
		b, _ := marshalJSON(tt.scheme)
		if string(b) != tt.want {
			t.Errorf("got %s, want %s", b, tt.want)
		}
	}
}

func TestAPIKeyAuthPanicsOnBadLocation(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	APIKeyAuth("body", "key")
}

// requireToken is the kind of middleware users pass next to Security.
func requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t0k" {
			WriteError(w, Unauthorized("unknown token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestSecurityWithMiddleware(t *testing.T) {
	api, _ := newTestAPI(WithSecurityScheme("bearer", BearerAuth("JWT")))
	secured := api.Group("/s", Security("bearer"), Middlewares(requireToken))
	Post(secured, "/hives", func(ctx context.Context, in hive) (hive, error) { return in, nil })
	Get(secured, "/public", func(ctx context.Context, _ struct{}) (string, error) { return "ok", nil }, Public())

	// Authentication runs before validation: an invalid body without a token gets 401.
	rec := do(t, api, request{method: "POST", target: "/s/hives", body: `{}`})
	expect(t, rec, 401, `{"type":"about:blank","title":"Unauthorized","status":401,"detail":"unknown token"}`)
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	expect(t, do(t, api, request{method: "POST", target: "/s/hives", body: `{}`,
		header: map[string]string{"Authorization": "Bearer t0k"}}), 422)
	expect(t, do(t, api, request{method: "GET", target: "/s/public"}), 401) // middlewares still apply

	doc, _ := api.OpenAPI()
	if !bytes.Contains(doc, []byte(`"bearer": []`)) || !bytes.Contains(doc, []byte(`"bearerFormat": "JWT"`)) {
		t.Errorf("security is not documented:\n%s", doc)
	}
}

func TestLogging(t *testing.T) {
	var logs bytes.Buffer
	api := New(Info{Title: "Test"}, WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	type none struct{}
	Get(api, "/fail", func(ctx context.Context, _ none) (string, error) { return "", errors.New("boom") })
	Get(api, "/missing", func(ctx context.Context, _ none) (string, error) { return "", NotFound("no") })
	expect(t, do(t, api, request{method: "GET", target: "/fail"}), 500)
	expect(t, do(t, api, request{method: "GET", target: "/missing"}), 404)
	out := logs.String()
	if !strings.Contains(out, `"error":"boom"`) || !strings.Contains(out, `"path":"/fail"`) {
		t.Errorf("500 not logged: %s", out)
	}
	if strings.Contains(out, "/missing") {
		t.Errorf("4xx must not be logged: %s", out)
	}

	silent := New(Info{Title: "Test"}, WithLogger(nil))
	Get(silent, "/panic", func(ctx context.Context, _ none) (string, error) { panic("queen escaped") })
	expect(t, do(t, silent, request{method: "GET", target: "/panic"}), 500)
}

func TestWriteErrorHidesInternalErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, errors.New("database password is hunter2"))
	expect(t, rec, 500, `"title":"Internal Server Error"`)
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("internal error leaked: %s", rec.Body)
	}
}
