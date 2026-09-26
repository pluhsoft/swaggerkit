---
title: Handlers
nav_order: 2
---

# Handlers

```go
func(ctx context.Context, in In) (Out, error)
```

Register with `Get`, `Post`, `Put`, `Patch`, `Delete` or `Handle(api, method, path, …)`. Paths use `net/http` patterns: `/hives/{hiveId}`, `/files/{path...}`.

Mistakes in routes and types panic at registration with the route and field in the message, as `http.ServeMux` does. They never reach production.

## Input

| Field                          | Source                         |
| ------------------------------ | ------------------------------ |
| `path:"hiveId"`                | path parameter, always required |
| `query:"limit"`                | query parameter                |
| `header:"X-Request-Id"`        | header                         |
| `cookie:"session"`             | cookie                         |
| field named `Body`             | JSON body; `*T` makes it optional |
| `form:"caption"`               | form field (`multipart/form-data` or urlencoded) |
| `form:"photo"` on `*multipart.FileHeader` | uploaded file; `[]*multipart.FileHeader` for several |
| embedded struct without tag    | its fields, e.g. shared pagination |

If the input has none of these, the whole input is the JSON body:

```go
func CreateHive(ctx context.Context, in NewHive) (Hive, error)
```

Parameter types: strings, numbers, booleans, `time.Time`, types with `UnmarshalText`, pointers to them (optional values) and slices for repeated query parameters and headers (`?tag=a&tag=b`). An empty query value (`?limit=`) counts as missing.

```go
type Pagination struct {
	Limit  int `query:"limit" validate:"min=1,max=100" default:"20"`
	Offset int `query:"offset" validate:"min=0" default:"0"`
}

type ListHivesInput struct {
	Pagination
	Status HiveStatus `query:"status"`
}

type UpdateHiveInput struct {
	HivePath          // path:"hiveId"
	Body HiveUpdate
}
```

Use `struct{}` for handlers without input.

### Uploads

```go
type UploadPhotoInput struct {
	HivePath
	Photo   *multipart.FileHeader `form:"photo" validate:"required" doc:"JPEG or PNG"`
	Caption string                `form:"caption" validate:"optional,max=100"`
}

swaggerkit.Put(api, "/hives/{hiveId}/photo", UploadPhoto, swaggerkit.MaxBodyBytes(2<<20))
```

- A route uses either `Body` or `form` fields.
- Form fields follow the parameter rules; files support `required` and `max` (number of files).
- `MaxBodyBytes` raises the 1 MiB limit for the route. Up to 32 MiB are kept in memory, the rest goes to temporary files that are removed after the handler.
- Check the file content, not the client's Content-Type: `http.DetectContentType`.

## Output

| Type                   | Response                                    |
| ---------------------- | ------------------------------------------- |
| any type               | JSON, status 200 or `Status(code)`          |
| `swaggerkit.NoContent` | 204                                         |
| `*swaggerkit.File`     | file or stream; ranges for `io.ReadSeeker`  |

A nil slice is sent as `[]`. A nil pointer or map without an error is a bug: 500 and a log record.

```go
return &swaggerkit.File{Body: f, ContentType: "image/png", Name: "hive.png", Inline: true}, nil
```

Document the content type with `swaggerkit.Produces("image/png")`.

## Errors

Return `*swaggerkit.Error` to choose the status. The client gets [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details (`application/problem+json`):

```go
return Hive{}, swaggerkit.NotFound("hive 7 does not exist")
return Hive{}, swaggerkit.NewError(http.StatusConflict, "only 6 kg of honey can be taken")
return Hive{}, &swaggerkit.Error{Status: 503, Detail: "try later", Err: err} // Err is logged, not sent
```

```json
{"type":"about:blank","title":"Not Found","status":404,"detail":"hive 7 does not exist"}
```

Any other error is logged and answered with 500 without details. Panics are recovered the same way.

| Status | When                                    |
| ------ | --------------------------------------- |
| 400    | malformed JSON                          |
| 413    | body larger than `WithMaxBodyBytes` (1 MiB) |
| 415    | body is not `application/json`          |
| 422    | validation failed, `errors` lists each value |

Document the errors a handler returns: `swaggerkit.Errors(http.StatusNotFound, http.StatusConflict)`. 401, 422 and a default error response are documented automatically.

## Route options

| Option                          | Effect                                   |
| ------------------------------- | ---------------------------------------- |
| `Summary`, `Description`        | text in the documentation; summary defaults to the handler name: `ListHives` → "List hives" |
| `OperationID`                   | defaults to `listHives`; must be unique  |
| `Tags`                          | groups operations                        |
| `Status`                        | success status                           |
| `Errors`, `Produces`            | documented responses                     |
| `Security`, `Public`            | documented [authentication](security)    |
| `Middlewares`                   | [middlewares](middleware) for the route  |
| `Deprecated`, `Hidden`          | mark or hide in the documentation        |

Groups share options and a path prefix:

```go
keeper := api.Group("/hives", swaggerkit.Tags("hives"), swaggerkit.Security("beekeeper"))
swaggerkit.Delete(keeper, "/{hiveId}", DeleteHive)
```

## Request context

```go
r := swaggerkit.Request(ctx)                        // *http.Request
swaggerkit.ResponseHeader(ctx).Set("X-Total", "42") // response headers
```
