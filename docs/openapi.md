---
title: OpenAPI
nav_order: 4
---

# OpenAPI

The OpenAPI 3.1 document is built from registered routes and their Go types. Older formats are converted from it for tools that need them.

| Format                        | Method / endpoint                                  |
| ----------------------------- | -------------------------------------------------- |
| OpenAPI 3.1                   | `api.OpenAPI()`, `/docs/openapi.json`              |
| OpenAPI 3.0                   | `api.Document(swaggerkit.FormatOpenAPI30)`, `/docs/openapi-3.0.json` |
| Swagger 2.0                   | `api.Document(swaggerkit.FormatSwagger20)`, `/docs/swagger.json` |

Swagger 2.0 cannot express everything: cookie parameters are dropped, bearer and OpenID Connect become an `Authorization` header API key, a response keeps one schema, nullable becomes `x-nullable`.

## Serving

```go
api := swaggerkit.New(info, swaggerkit.WithDocs("/docs"))
```

| Path                      | Content          |
| ------------------------- | ---------------- |
| `/docs`                   | Swagger UI       |
| `/docs/openapi.json`      | OpenAPI 3.1      |
| `/docs/openapi-3.0.json`  | OpenAPI 3.0      |
| `/docs/swagger.json`      | Swagger 2.0      |

Swagger UI loads from jsDelivr with a pinned version and Subresource Integrity. Without internet access, or to serve the document elsewhere, mount `api.OpenAPIHandler()` and your own UI.

Docs are off unless `WithDocs` is set: decide whether production exposes them.

## Generating a file

The document needs no running server: build the API and write it.

```go
// main.go
if *specFile != "" {
	f, _ := os.Create(*specFile)
	defer f.Close()
	api.WriteOpenAPI(f)
	return
}
```

```sh
go run ./cmd/server -openapi openapi.json
```

Commit the file and keep it current with a golden test. CI then fails on unreviewed API changes; client generators and other services use the file.

```go
var update = flag.Bool("update", false, "rewrite openapi.json")

func TestOpenAPI(t *testing.T) {
	doc, _ := NewAPI().OpenAPI()
	if *update {
		os.WriteFile("openapi.json", doc, 0o644)
	}
	want, _ := os.ReadFile("openapi.json")
	if !bytes.Equal(doc, want) {
		t.Error("openapi.json is outdated; run go test -run TestOpenAPI -update")
	}
}
```

[`examples/apiary`](https://github.com/pluhsoft/swaggerkit/tree/main/examples/apiary) does both.

## Document options

| Option                      | Effect                                                      |
| --------------------------- | ----------------------------------------------------------- |
| `Info{…}`                   | title, version, description, contact, license               |
| `WithServer(url, desc)`     | server URLs                                                 |
| `WithBasePath("/api/v1")`   | routes are served under the prefix; paths stay short, servers get the prefix |
| `WithTag(name, desc)`       | tag descriptions and order                                  |

## Schemas

- Named structs, enums and `SchemaProvider` types become `components/schemas`, referenced by name.
- Generic types get readable names: `Page[Hive]` → `PageHive`.
- Types with the same name from different packages get the package name as a prefix.
- Recursive types are supported.
- A pointer field without `omitempty` is nullable: Go encodes nil as `null`.
- Errors use the shared `Problem` schema.

## Lint

`api.Lint()` checks the API design and returns issues with a severity, rule, location and message.

```sh
go run ./examples/apiary -lint
```

| Rule                    | Finds                                                  |
| ----------------------- | ------------------------------------------------------ |
| `verb-in-path`          | `/project/delete/{id}` instead of `DELETE /projects/{id}` |
| `path-case`, `path-param-case` | `/Hives`, `{Project_ID}`                        |
| `unsecured-write`       | write operation without security when schemes exist    |
| `security-not-enforced` | `Security` without a middleware that checks it         |
| `api-key-in-query`      | API keys that end up in logs                           |
| `unbounded-string`, `unbounded-array`, `unbounded-map` | request input without `max` |
| `post-status`           | POST that creates a resource and answers 200           |
| `array-response`        | top-level array responses that cannot get pagination later |
| `delete-body`           | DELETE with a body                                     |
| `untyped-value`         | `any` fields and custom `MarshalJSON` without a schema |
| `operation-tags`, `type-doc`, `field-doc` | missing tags, type and field descriptions |
| `info-title`, `info-version` | incomplete `Info`                                 |

Fail CI on errors or on any issue, as you prefer:

```go
func TestLint(t *testing.T) {
	for _, issue := range NewAPI().Lint() {
		t.Error(issue)
	}
}
```
