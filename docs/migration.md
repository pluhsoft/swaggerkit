---
title: Migration
nav_order: 7
---

# Migration

swaggerkit is an `http.Handler`. Mount it in any router and move routes one by one.

## Mounting

```go
api := swaggerkit.New(info, swaggerkit.WithBasePath("/api/v1"))

http.Handle("/api/v1/", api)                    // net/http
r.PathPrefix("/api/v1/").Handler(api)           // gorilla/mux
r.Mount("/api/v1", api)                         // chi
g.Any("/api/v1/*path", gin.WrapH(api))          // gin
e.Any("/api/v1/*", echo.WrapHandler(api))       // echo
```

The routers pass the full path, so the base path stays in `WithBasePath`.

## From handlers with separate params, query and body

Typical wrapper code:

```go
WrapHandlerFunc(router, "/project/{Id}", MethodPOST, UpdateProjectById, middleware.Auth)

func UpdateProjectById(w http.ResponseWriter, r *http.Request,
	args HandlerFuncArguments[Params, Query, Body]) HandlerFuncResponse[Project]
```

swaggerkit:

```go
type UpdateProjectInput struct {
	ID   int64 `path:"id" validate:"min=1"` // was Params
	Query                                    // embedded, fields tagged query:"…"
	Body ProjectUpdate                       // was Body
}

func UpdateProject(ctx context.Context, in UpdateProjectInput) (Project, error)

swaggerkit.Patch(projects, "/{id}", UpdateProject)
```

| Before                                   | swaggerkit                                        |
| ---------------------------------------- | ------------------------------------------------- |
| `HandlerFuncResponse{Status, Response}`  | return value, `Status(code)` option               |
| `HandlerFuncResponse{Error}`             | return `*swaggerkit.Error` or any error           |
| `IsFileResponse`, `FilePath`             | return `*swaggerkit.File` with an `io.Reader`     |
| Go field names as parameter names        | `path:"id"`, `query:"limit"`                      |
| `validate:"required"`, `min:"1"`, `email:"true"` | `validate:"required,min=1,email"`         |
| enums parsed from source files           | `Enum()` method on the type                       |
| auth detected by middleware name         | `Security` + your auth middleware in `Middlewares` |
| `ResponseCORSAllowed` per route          | any CORS middleware in `api.Use`, e.g. rs/cors    |
| env vars `SWAGGER_*`                     | options: `WithMaxBodyBytes`, `WithUnknownFields`  |
| `logrus.Entry`                           | `WithLogger(*slog.Logger)` or `nil`               |
| `GenerateSwaggerJSON` writes a file      | `api.WriteOpenAPI` or `WithDocs`                  |
| actions in paths: `POST /project/delete/{Id}` | methods: `DELETE /projects/{id}` (see `Lint`) |

## From gorilla/mux

- Path regexps `{id:[0-9]+}` are not supported: use `{id}` and `validate:"min=1"` on an integer field.
- `mux.Vars(r)` → fields tagged `path`.
- `Methods("GET")` → `swaggerkit.Get`.
- `Subrouter()` → `api.Group(prefix, options…)`.

## From swaggo/swag

Delete the `// @Param`, `// @Success` and `// @Router` comments and the `swag init` step. The same information comes from the handler types; the document is always current.

## From chi, gin, echo handlers

Handlers become functions of input and output. Binding (`c.ShouldBindJSON`, `c.Param`, `chi.URLParam`) and response writing (`c.JSON`, `render.JSON`) move into the types. Keep the router for routes you have not moved yet.
