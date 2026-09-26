---
title: Middleware and logging
nav_order: 6
---

# Middleware and logging

## Middleware

`swaggerkit.Middleware` is `func(http.Handler) http.Handler`: middlewares from chi, gorilla and the standard library work as is.

```go
api.Use(requestID, swaggerkit.RequestLogger(logger))     // every request, incl. 404 and preflight
hives := api.Group("/hives", swaggerkit.Middlewares(rateLimit))
swaggerkit.Post(hives, "", CreateHive, swaggerkit.Middlewares(audit))
```

Order: `api.Use` (first is outermost) → group → route → authentication → validation → handler.

## Logging

swaggerkit uses `log/slog`. The default is `slog.Default()`.

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
api := swaggerkit.New(info, swaggerkit.WithLogger(logger))
```

| Level | Record                                                 |
| ----- | ------------------------------------------------------ |
| Error | handler errors (5xx) with the cause, panics with stack |
| Debug | rejected requests (4xx), rejected credentials          |

`RequestLogger(logger)` adds one record per request: method, path, status, bytes, duration.

### Your fields

Records are written with the request context. Add fields in a middleware:

```go
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := swaggerkit.AppendLogAttrs(r.Context(), slog.String("request_id", newID()))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

Or read the context in your own `slog.Handler`.

### Other loggers

zap, zerolog and logrus have `slog.Handler` adapters (`zapslog`, `slog-zerolog`, `slog-logrus`). Wrap your logger and pass it to `WithLogger`.
