package swaggerkit

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// SecurityScheme describes how clients authenticate. Create it with
// [BearerAuth], [BasicAuth], [APIKeyAuth] or [OpenIDConnectAuth] and register
// it with [WithSecurityScheme].
//
// When the scheme has a verify function, swaggerkit checks credentials on
// every route that requires the scheme, so the documentation and the
// behavior cannot diverge. With a nil verify function the scheme is only
// documented and a middleware must enforce it.
type SecurityScheme struct {
	Type             string `json:"type"` // "http", "apiKey" or "openIdConnect"
	Description      string `json:"description,omitempty"`
	Scheme           string `json:"scheme,omitempty"`       // "bearer" or "basic" for Type "http"
	BearerFormat     string `json:"bearerFormat,omitempty"` // e.g. "JWT"
	In               string `json:"in,omitempty"`           // "header", "query" or "cookie" for Type "apiKey"
	Name             string `json:"name,omitempty"`         // header, query or cookie name for Type "apiKey"
	OpenIDConnectURL string `json:"openIdConnectUrl,omitempty"`

	extract func(r *http.Request) (credential, bool)
	verify  func(ctx context.Context, c credential) (context.Context, error)
}

type credential struct {
	value, username, password string
}

// Verifier checks a token or key. It returns a context for the handler,
// e.g. with the authenticated user, or an error. An *Error chooses the
// status code (for example 403); any other error results in 401.
type Verifier func(ctx context.Context, token string) (context.Context, error)

// BearerAuth returns an HTTP bearer scheme ("Authorization: Bearer <token>").
func BearerAuth(verify Verifier) SecurityScheme {
	return SecurityScheme{
		Type:    "http",
		Scheme:  "bearer",
		extract: bearerToken,
		verify:  wrapVerifier(verify),
	}
}

// BasicAuth returns an HTTP basic scheme. Use it over HTTPS only.
func BasicAuth(verify func(ctx context.Context, username, password string) (context.Context, error)) SecurityScheme {
	s := SecurityScheme{
		Type:   "http",
		Scheme: "basic",
		extract: func(r *http.Request) (credential, bool) {
			u, p, ok := r.BasicAuth()
			return credential{username: u, password: p}, ok
		},
	}
	if verify != nil {
		s.verify = func(ctx context.Context, c credential) (context.Context, error) {
			return verify(ctx, c.username, c.password)
		}
	}
	return s
}

// APIKeyAuth returns an API key scheme. in is "header", "query" or "cookie";
// name is the header, query parameter or cookie name.
// Prefer headers: query strings end up in access logs.
func APIKeyAuth(in, name string, verify Verifier) SecurityScheme {
	s := SecurityScheme{Type: "apiKey", In: in, Name: name, verify: wrapVerifier(verify)}
	switch in {
	case inHeader:
		s.extract = func(r *http.Request) (credential, bool) {
			v := r.Header.Get(name)
			return credential{value: v}, v != ""
		}
	case inQuery:
		s.extract = func(r *http.Request) (credential, bool) {
			v := r.URL.Query().Get(name)
			return credential{value: v}, v != ""
		}
	case inCookie:
		s.extract = func(r *http.Request) (credential, bool) {
			c, err := r.Cookie(name)
			if err != nil || c.Value == "" {
				return credential{}, false
			}
			return credential{value: c.Value}, true
		}
	default:
		panic(`swaggerkit: APIKeyAuth: in must be "header", "query" or "cookie"`)
	}
	return s
}

// OpenIDConnectAuth returns an OpenID Connect scheme. discoveryURL points to
// the provider's /.well-known/openid-configuration. The bearer token is passed
// to verify.
func OpenIDConnectAuth(discoveryURL string, verify Verifier) SecurityScheme {
	return SecurityScheme{
		Type:             "openIdConnect",
		OpenIDConnectURL: discoveryURL,
		extract:          bearerToken,
		verify:           wrapVerifier(verify),
	}
}

func wrapVerifier(verify Verifier) func(context.Context, credential) (context.Context, error) {
	if verify == nil {
		return nil
	}
	return func(ctx context.Context, c credential) (context.Context, error) { return verify(ctx, c.value) }
}

func bearerToken(r *http.Request) (credential, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "bearer") || token == "" {
		return credential{}, false
	}
	return credential{value: token}, true
}

// authenticate accepts the request if any of the schemes verifies it.
func (a *API) authenticate(ctx context.Context, r *http.Request, names []string) (context.Context, error) {
	var failure error
	enforced := false
	for _, name := range names {
		s := a.securitySchemes[name]
		if s.verify == nil {
			continue
		}
		enforced = true
		c, ok := s.extract(r)
		if !ok {
			continue
		}
		authCtx, err := s.verify(ctx, c)
		if err == nil {
			if authCtx == nil {
				authCtx = ctx
			}
			return authCtx, nil
		}
		if failure == nil {
			failure = err
		}
	}
	if !enforced {
		return ctx, nil // documented only; a middleware enforces it
	}
	if w, ok := ctx.Value(writerKey).(http.ResponseWriter); ok {
		if challenge := a.challenge(names); challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
	}
	if failure == nil {
		return nil, Unauthorized("authentication required")
	}
	var e *Error
	if errors.As(failure, &e) {
		return nil, e
	}
	a.log(ctx, slog.LevelDebug, "swaggerkit: credentials rejected", slog.Any("error", failure))
	return nil, Unauthorized("invalid credentials")
}

func (a *API) challenge(names []string) string {
	for _, name := range names {
		s := a.securitySchemes[name]
		switch {
		case s.Type == "http" && s.Scheme == "bearer", s.Type == "openIdConnect":
			return "Bearer"
		case s.Type == "http" && s.Scheme == "basic":
			return `Basic realm="api", charset="UTF-8"`
		}
	}
	return ""
}
