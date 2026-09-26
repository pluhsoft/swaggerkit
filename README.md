# swaggerkit

[![Go Reference](https://pkg.go.dev/badge/github.com/pluhsoft/swaggerkit.svg)](https://pkg.go.dev/github.com/pluhsoft/swaggerkit)
[![CI](https://github.com/pluhsoft/swaggerkit/actions/workflows/ci.yml/badge.svg)](https://github.com/pluhsoft/swaggerkit/actions/workflows/ci.yml)

Typed HTTP handlers for Go with request validation and OpenAPI (Swagger) generated from the same types.
The documentation cannot drift from the code.

- `net/http` only, no dependencies
- Path, query, header, cookie and JSON body binding
- Validation from struct tags, RFC 9457 error responses
- OpenAPI 3.1, built-in Swagger UI
- Security schemes that are documented and enforced
- CORS, `log/slog` logging, any `net/http` middleware
- `Lint` hints that improve API design

## Install

```sh
go get github.com/pluhsoft/swaggerkit
```

Requires Go 1.24 or later.

## Example

```go
type Hive struct {
	ID   int64  `json:"id" example:"1"`
	Name string `json:"name" doc:"Name painted on the hive" validate:"min=1,max=64"`
	Bees int    `json:"bees" validate:"min=0"`
}

type GetHiveInput struct {
	ID int64 `path:"hiveId" validate:"min=1"`
}

func GetHive(ctx context.Context, in GetHiveInput) (Hive, error) {
	hive, ok := hives[in.ID]
	if !ok {
		return Hive{}, swaggerkit.NotFound("hive not found")
	}
	return hive, nil
}

func main() {
	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"},
		swaggerkit.WithDocs("/docs"))
	swaggerkit.Get(api, "/hives/{hiveId}", GetHive, swaggerkit.Tags("hives"))
	http.ListenAndServe(":8080", api) // Swagger UI: http://localhost:8080/docs
}
```

`GET /hives/0` answers `422` without calling the handler:

```json
{"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"request validation failed",
 "errors":[{"location":"path.hiveId","message":"must be greater than or equal to 1"}]}
```

A complete API with auth, pagination, files and tests: [`examples/apiary`](examples/apiary).

## Documentation

- [Guide](https://pluhsoft.github.io/swaggerkit/): handlers, validation, OpenAPI, security, middleware
- [Migration](https://pluhsoft.github.io/swaggerkit/migration) from gorilla/mux, chi, gin, echo and swaggo
- [API reference](https://pkg.go.dev/github.com/pluhsoft/swaggerkit)
- [Example API in Swagger UI](https://pluhsoft.github.io/swaggerkit/demo/)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues: [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
