---
title: Security
nav_order: 5
---

# Security

## Authentication

swaggerkit documents authentication; your middleware checks it. Put both on the same group so they cannot drift apart.

```go
api := swaggerkit.New(info, swaggerkit.WithSecurityScheme("jwt", swaggerkit.BearerAuth("JWT")))

keeper := api.Group("/hives", swaggerkit.Security("jwt"), swaggerkit.Middlewares(requireJWT))
swaggerkit.Delete(keeper, "/{hiveId}", DeleteHive)
```

| Constructor                        | Documents                                   |
| ---------------------------------- | ------------------------------------------- |
| `BearerAuth("JWT")`                | `Authorization: Bearer <token>`             |
| `BasicAuth()`                      | `Authorization: Basic …`                    |
| `APIKeyAuth("header", "X-API-Key")`| API key in a header, `query` or `cookie`    |
| `OpenIDConnectAuth(discoveryURL)`  | OpenID Connect                              |

- `Security("a", "b")` documents that either scheme is accepted; `Public()` removes an inherited requirement.
- Routes with `Security` document a 401 response.
- Middlewares run before validation, so unauthenticated requests never reach body parsing.
- `Lint` warns about routes with `Security` but no middleware (`security-not-enforced`).
- Compare secrets with `crypto/subtle.ConstantTimeCompare`.

A middleware passes the user to the handler through the request context:

```go
func requireJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := parseJWT(r.Header.Get("Authorization"))
		if err != nil {
			swaggerkit.WriteError(w, swaggerkit.Unauthorized("invalid token")) // same format as handler errors
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}
```

## Defaults

- Bodies over 1 MiB: 413. Bodies must be `application/json`: blocks form-based CSRF.
- Unknown JSON fields are rejected.
- Internal errors and panics are logged, never sent to clients. Validation errors do not echo values.
- Responses carry `X-Content-Type-Options: nosniff`; files are sent as attachments unless `Inline`.
- CORS, rate limits and timeouts belong to your middlewares and `http.Server`.
