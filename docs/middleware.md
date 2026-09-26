---
title: Middleware and logging
nav_order: 6
---

# Middleware and logging

## Middleware

`swaggerkit.Middleware` is `func(http.Handler) http.Handler`: middlewares from the standard library, chi, gorilla and [rs/cors](https://github.com/rs/cors) work as is.

```go
api.Use(requestID, cors.Default().Handler)          // every request, incl. 404 and CORS preflight
hives := api.Group("/hives", swaggerkit.Middlewares(requireJWT))
swaggerkit.Post(hives, "", CreateHive, swaggerkit.Middlewares(audit))
```

Order: `api.Use` (first is outermost) → group → route → validation → handler.

## Logging

swaggerkit logs only what the client cannot see: handler errors that become 500 and panics, with the cause and stack. It uses `log/slog` from the standard library.

| Setting                         | Effect                    |
| ------------------------------- | ------------------------- |
| default                         | `slog.Default()`          |
| `WithLogger(logger)`            | your `*slog.Logger`       |
| `WithLogger(nil)`               | logging off               |

Records are written with the request context: a `slog.Handler` that reads the context can add a request ID or user. zap, zerolog and logrus have `slog.Handler` adapters.

Access logs are a middleware's job.
