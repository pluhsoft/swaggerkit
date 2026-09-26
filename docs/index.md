---
title: Quick start
nav_order: 1
---

# swaggerkit

Typed HTTP handlers for Go. Request binding, validation and the OpenAPI (Swagger) document come from the same Go types, so the documentation cannot drift from the code. Only `net/http`, no dependencies.

```sh
go get github.com/pluhsoft/swaggerkit
```

## First API

```go
package main

import (
	"context"
	"net/http"

	"github.com/pluhsoft/swaggerkit"
)

type Hive struct {
	ID   int64  `json:"id" example:"1"`
	Name string `json:"name" doc:"Name painted on the hive" validate:"min=1,max=64"`
	Bees int    `json:"bees" doc:"Bees living in the hive" validate:"min=0"`
}

type HivePath struct {
	ID int64 `path:"hiveId" validate:"min=1"`
}

func GetHive(ctx context.Context, in HivePath) (Hive, error) {
	if in.ID != 1 {
		return Hive{}, swaggerkit.NotFound("hive not found")
	}
	return Hive{ID: 1, Name: "Linden", Bees: 42000}, nil
}

type NewHive struct {
	Name string `json:"name" validate:"min=1,max=64"`
}

func CreateHive(ctx context.Context, in NewHive) (Hive, error) {
	return Hive{ID: 2, Name: in.Name}, nil
}

func main() {
	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"},
		swaggerkit.WithDocs("/docs"))

	hives := api.Group("/hives", swaggerkit.Tags("hives"))
	swaggerkit.Get(hives, "/{hiveId}", GetHive)
	swaggerkit.Post(hives, "", CreateHive, swaggerkit.Status(http.StatusCreated))

	http.ListenAndServe(":8080", api)
}
```

- Swagger UI: `http://localhost:8080/docs`
- OpenAPI 3.1: `/docs/openapi.json`, OpenAPI 3.0: `/docs/openapi-3.0.json`

## What happens on a request

1. Middlewares added with `api.Use`, then group and route middlewares.
2. Authentication, if the route has [security](security).
3. Parameters and body are parsed and [validated](validation). Invalid input gets `422` with every problem listed; the handler is not called.
4. The handler runs. Its result is encoded as JSON, its error as [problem details](handlers#errors).

## Next

- [Handlers](handlers): inputs, outputs, errors
- [Validation](validation): tags and rules
- [OpenAPI](openapi): documents, Swagger UI, generation, lint
- [Security](security): authentication, CORS
- [Middleware and logging](middleware)
- [Migration](migration) from gorilla/mux, chi, gin, echo, swaggo
- [Example API](demo/) generated from [`examples/apiary`](https://github.com/pluhsoft/swaggerkit/tree/main/examples/apiary)
