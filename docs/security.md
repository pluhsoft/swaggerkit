---
title: Security
nav_order: 5
---

# Security

## Authentication

Register a scheme with a verify function and require it on routes. swaggerkit documents the scheme and checks every request, so the documentation and the behavior match.

```go
verify := func(ctx context.Context, token string) (context.Context, error) {
	user, err := users.ByToken(ctx, token)
	if err != nil {
		return nil, swaggerkit.Unauthorized("unknown token")
	}
	if user.Banned {
		return nil, swaggerkit.Forbidden("account is blocked")
	}
	return context.WithValue(ctx, userKey{}, user), nil
}

jwt := swaggerkit.BearerAuth(verify)
jwt.BearerFormat = "JWT"

api := swaggerkit.New(info, swaggerkit.WithSecurityScheme("jwt", jwt))

keeper := api.Group("/hives", swaggerkit.Security("jwt"))
swaggerkit.Delete(keeper, "/{hiveId}", DeleteHive)
swaggerkit.Get(keeper, "/{hiveId}", GetHive, swaggerkit.Public()) // no auth
```

| Constructor                           | Credentials                          |
| ------------------------------------- | ------------------------------------ |
| `BearerAuth(verify)`                  | `Authorization: Bearer <token>`      |
| `BasicAuth(verify)`                   | `Authorization: Basic …`, HTTPS only |
| `APIKeyAuth("header", "X-API-Key", verify)` | header, `query` or `cookie`    |
| `OpenIDConnectAuth(discoveryURL, verify)`   | bearer token, OIDC discovery in the document |

- The context returned by verify reaches the handler.
- An `*Error` from verify sets the status (403); any other error gives 401 without details.
- `Security("a", "b")` accepts either scheme.
- Authentication runs before the body is read.
- With a nil verify function the scheme is only documented; `Lint` warns about it.
- Compare secrets with `crypto/subtle.ConstantTimeCompare`.

## CORS

```go
api.Use(swaggerkit.CORS(swaggerkit.CORSOptions{
	AllowedOrigins:   []string{"https://app.example.com"},
	AllowCredentials: true,
}))
```

Add it with `api.Use` so preflight requests reach it. Defaults: methods GET, HEAD, POST, PUT, PATCH, DELETE; headers `Content-Type`, `Authorization`; preflight cache 10 minutes. `"*"` with credentials panics. Preflights with unknown origins, methods or headers get 403.

## Defaults

- Bodies over 1 MiB: 413. Bodies must be `application/json`: blocks form-based CSRF.
- Unknown JSON fields are rejected.
- Internal errors and panics are logged, never sent to clients. Validation errors do not echo values.
- Responses carry `X-Content-Type-Options: nosniff`; files are sent as attachments unless `Inline`.
- Set `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and `IdleTimeout` on `http.Server`.
