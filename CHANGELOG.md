# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versions follow
[Semantic Versioning](https://semver.org).

## [Unreleased]

### Added

- `WriteError` sends problem details from middlewares (#7).
- `Doc() string` method describes a type in the OpenAPI document; `Lint` rule `type-doc` (#10).
- `Statuses` and `SetStatus` for routes with more than one success status (#9).

### Changed

- Faster request validation: up to 29% less time and 44% fewer allocations; benchmarks in `bench_test.go` (#11).

## [1.0.0] - 2026-09-26

### Added

- Typed handlers on `net/http`: `Get`, `Post`, `Put`, `Patch`, `Delete`, `Handle`, groups and middlewares.
- Path, query, header, cookie and JSON body binding with validation from `validate`, `pattern` and `default` tags.
- RFC 9457 problem details for errors.
- OpenAPI 3.1 generation, Swagger UI, user enums, `SchemaProvider`.
- Security schemes in the document: bearer, basic, API key, OpenID Connect.
- Middlewares for the API, groups and routes; server errors logged with `log/slog`.
- `API.Lint` for API design hints.
- `examples/apiary`.
