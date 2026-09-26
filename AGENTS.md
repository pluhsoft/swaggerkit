# Guide for AI agents

swaggerkit is a single Go package: typed `net/http` handlers, validation and OpenAPI generation from Go types.
Scope: validation and generation only. Authentication, CORS and access logs are the user's middlewares.

## Rules

- No dependencies outside the standard library. `go.mod` has no `require`.
- Minimum Go version is in `go.mod`; do not use newer language or library features.
- Public API changes follow SemVer; `gorelease` checks them on release.
- Every change: tests, `CHANGELOG.md` line under `## [Unreleased]`, docs in `docs/` if behavior changes.
- Registration mistakes panic with the route and field in the message. Request mistakes return problem details. Never send internal error text to clients.
- Work on an issue branch, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Map

| File                  | Contents                                                  |
| --------------------- | --------------------------------------------------------- |
| `api.go`              | `API`, `New`, options, groups                             |
| `route.go`            | `Handle`/`Get`/…, route options, naming                   |
| `bind.go`             | input analysis, parameter and body binding                |
| `handler.go`          | request pipeline, outputs, context helpers                |
| `schema.go`           | `Schema` and its JSON form                                |
| `schemagen.go`        | Go type → schema, components, enums, `Doc()`              |
| `fields.go`           | encoding/json field rules                                 |
| `tags.go`             | `validate`, `doc`, `example`, `default`, `pattern` tags   |
| `validate.go`         | JSON value validation                                     |
| `formats.go`          | string formats                                            |
| `errors.go`           | `Error`, problem details                                  |
| `security.go`         | security schemes for the document                         |
| `openapi.go`, `docs.go` | OpenAPI 3.1 document types, Swagger UI                  |
| `lint.go`             | `API.Lint`                                                |
| `examples/apiary`     | example API, golden `openapi.json`                        |
| `internal/release`    | release tool used by CI                                   |

## Commands

```sh
go test -race ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go test ./examples/apiary -run TestOpenAPI -update
go run ./examples/apiary -lint
```
