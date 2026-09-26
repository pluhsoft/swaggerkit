package swaggerkit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

type userKey struct{}

func verifyToken(want string) Verifier {
	return func(ctx context.Context, token string) (context.Context, error) {
		switch token {
		case want:
			return context.WithValue(ctx, userKey{}, "queen"), nil
		case "banned":
			return nil, Forbidden("banned")
		}
		return nil, errors.New("bad token")
	}
}

func TestSecurity(t *testing.T) {
	basic := BasicAuth(func(ctx context.Context, user, pass string) (context.Context, error) {
		if user == "keeper" && pass == "honey" {
			return context.WithValue(ctx, userKey{}, user), nil
		}
		return nil, errors.New("no")
	})
	api, _ := newTestAPI(
		WithSecurityScheme("bearer", BearerAuth(verifyToken("t0k"))),
		WithSecurityScheme("basic", basic),
		WithSecurityScheme("header", APIKeyAuth("header", "X-API-Key", verifyToken("k1"))),
		WithSecurityScheme("query", APIKeyAuth("query", "key", verifyToken("k2"))),
		WithSecurityScheme("cookie", APIKeyAuth("cookie", "sid", verifyToken("k3"))),
		WithSecurityScheme("oidc", OpenIDConnectAuth("https://id.example/.well-known/openid-configuration", verifyToken("o1"))),
		WithSecurityScheme("docs-only", BearerAuth(nil)),
	)
	type none struct{}
	whoami := func(ctx context.Context, _ none) (string, error) {
		user, _ := ctx.Value(userKey{}).(string)
		return user, nil
	}
	secured := api.Group("/s", Security("bearer", "basic"))
	Get(secured, "/me", whoami)
	Get(secured, "/public", whoami, Public())
	Get(api, "/header", whoami, Security("header"))
	Get(api, "/query", whoami, Security("query"))
	Get(api, "/cookie", whoami, Security("cookie"))
	Get(api, "/oidc", whoami, Security("oidc"))
	Get(api, "/docs-only", whoami, Security("docs-only"))

	rec := do(t, api, request{method: "GET", target: "/s/me"})
	expect(t, rec, 401, "authentication required")
	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
	expect(t, do(t, api, request{method: "GET", target: "/s/me", header: map[string]string{"Authorization": "Bearer t0k"}}), 200, "queen")
	expect(t, do(t, api, request{method: "GET", target: "/s/me", header: map[string]string{"Authorization": "bearer   t0k"}}), 200, "queen")
	expect(t, do(t, api, request{method: "GET", target: "/s/me", header: map[string]string{"Authorization": "Bearer nope"}}), 401, "invalid credentials")
	expect(t, do(t, api, request{method: "GET", target: "/s/me", header: map[string]string{"Authorization": "Bearer banned"}}), 403, "banned")
	expect(t, do(t, api, request{method: "GET", target: "/s/me", header: map[string]string{"Authorization": "Basic a2VlcGVyOmhvbmV5"}}), 200, "keeper")
	expect(t, do(t, api, request{method: "GET", target: "/s/public"}), 200)
	expect(t, do(t, api, request{method: "GET", target: "/header", header: map[string]string{"X-API-Key": "k1"}}), 200, "queen")
	expect(t, do(t, api, request{method: "GET", target: "/query?key=k2"}), 200, "queen")
	expect(t, do(t, api, request{method: "GET", target: "/cookie", header: map[string]string{"Cookie": "sid=k3"}}), 200, "queen")
	expect(t, do(t, api, request{method: "GET", target: "/cookie"}), 401)
	expect(t, do(t, api, request{method: "GET", target: "/oidc", header: map[string]string{"Authorization": "Bearer o1"}}), 200, "queen")
	expect(t, do(t, api, request{method: "GET", target: "/docs-only"}), 200)

	basicOnly, _ := newTestAPI(WithSecurityScheme("basic", basic))
	Get(basicOnly, "/me", whoami, Security("basic"))
	rec = do(t, basicOnly, request{method: "GET", target: "/me"})
	if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Basic") {
		t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
}

func TestAPIKeyAuthPanicsOnBadLocation(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	APIKeyAuth("body", "key", nil)
}

func TestCORS(t *testing.T) {
	api, _ := newTestAPI()
	api.Use(CORS(CORSOptions{
		AllowedOrigins:   []string{"https://app.example"},
		ExposedHeaders:   []string{"X-Total"},
		AllowCredentials: true,
		MaxAge:           time.Hour,
	}))
	type none struct{}
	Post(api, "/hives", func(ctx context.Context, _ none) (string, error) { return "ok", nil })

	preflight := func(origin, method, headers string) map[string]string {
		return map[string]string{"Origin": origin, "Access-Control-Request-Method": method, "Access-Control-Request-Headers": headers}
	}
	rec := do(t, api, request{method: "OPTIONS", target: "/hives", header: preflight("https://app.example", "POST", "Content-Type, authorization")})
	expect(t, rec, 204)
	h := rec.Header()
	if h.Get("Access-Control-Allow-Origin") != "https://app.example" || h.Get("Access-Control-Allow-Credentials") != "true" ||
		h.Get("Access-Control-Max-Age") != "3600" || !strings.Contains(h.Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Errorf("preflight headers = %v", h)
	}
	expect(t, do(t, api, request{method: "OPTIONS", target: "/hives", header: preflight("https://evil.example", "POST", "")}), 403)
	expect(t, do(t, api, request{method: "OPTIONS", target: "/hives", header: preflight("https://app.example", "TRACE", "")}), 403)
	expect(t, do(t, api, request{method: "OPTIONS", target: "/hives", header: preflight("https://app.example", "POST", "X-Secret")}), 403)

	rec = do(t, api, request{method: "POST", target: "/hives", header: map[string]string{"Origin": "https://app.example"}})
	expect(t, rec, 200)
	if rec.Header().Get("Access-Control-Expose-Headers") != "X-Total" {
		t.Errorf("headers = %v", rec.Header())
	}
	rec = do(t, api, request{method: "POST", target: "/hives", header: map[string]string{"Origin": "https://evil.example"}})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin got CORS headers")
	}

	wildcard, _ := newTestAPI()
	wildcard.Use(CORS(CORSOptions{AllowedOrigins: []string{"*"}}))
	rec = do(t, wildcard, request{method: "OPTIONS", target: "/x", header: preflight("https://any.example", "GET", "")})
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("wildcard = %v", rec.Header())
	}

	defer func() {
		if recover() == nil {
			t.Error("wildcard with credentials must panic")
		}
	}()
	CORS(CORSOptions{AllowedOrigins: []string{"*"}, AllowCredentials: true})
}

func TestLogging(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	api := New(Info{Title: "Test"}, WithLogger(logger))
	api.Use(RequestLogger(logger), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(AppendLogAttrs(r.Context(), slog.String("request_id", "r-42"))))
		})
	})
	type none struct{}
	Get(api, "/fail", func(ctx context.Context, _ none) (string, error) { return "", errors.New("boom") })
	expect(t, do(t, api, request{method: "GET", target: "/fail"}), 500)
	out := logs.String()
	for _, want := range []string{`"request_id":"r-42"`, `"error":"boom"`, `"msg":"http request"`, `"status":500`} {
		if !strings.Contains(out, want) {
			t.Errorf("logs %s do not contain %s", out, want)
		}
	}
}
