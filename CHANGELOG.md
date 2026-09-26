# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versions follow
[Semantic Versioning](https://semver.org).

## [Unreleased]

### Added

- Typed handlers on `net/http`: `Get`, `Post`, `Put`, `Patch`, `Delete`, `Handle`, groups and middlewares.
- Path, query, header, cookie and JSON body binding with validation from `validate`, `pattern` and `default` tags.
- RFC 9457 problem details for errors.
- OpenAPI 3.1 and 3.0 generation, Swagger UI, user enums, `SchemaProvider`.
- Security schemes: bearer, basic, API key, OpenID Connect.
- `CORS` and `RequestLogger` middlewares, `log/slog` logging.
- `API.Lint` for API design hints.
- `examples/apiary`.
