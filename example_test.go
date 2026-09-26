package swaggerkit_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/pluhsoft/swaggerkit"
)

type Hive struct {
	ID   int    `json:"id" doc:"Hive ID" example:"1"`
	Name string `json:"name" doc:"Name painted on the hive" validate:"min=1,max=64" example:"Linden"`
	Bees int    `json:"bees" doc:"Bees in the hive" validate:"min=0"`
}

type GetHiveInput struct {
	HiveID int `path:"hiveId" validate:"min=1"`
}

func GetHive(ctx context.Context, in GetHiveInput) (Hive, error) {
	if in.HiveID != 1 {
		return Hive{}, swaggerkit.NotFound("hive not found")
	}
	return Hive{ID: 1, Name: "Linden", Bees: 42000}, nil
}

func Example() {
	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"},
		swaggerkit.WithDocs("/docs"))
	swaggerkit.Get(api, "/hives/{hiveId}", GetHive, swaggerkit.Tags("hives"))

	for _, path := range []string{"/hives/1", "/hives/2", "/hives/0"} {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		fmt.Print(rec.Code, " ", rec.Body.String())
	}
	// Output:
	// 200 {"id":1,"name":"Linden","bees":42000}
	// 404 {"type":"about:blank","title":"Not Found","status":404,"detail":"hive not found"}
	// 422 {"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"request validation failed","errors":[{"location":"path.hiveId","message":"must be greater than or equal to 1"}]}
}

type NewHive struct {
	Name string `json:"name" validate:"min=1,max=64"`
}

// A handler whose input has no path, query, header or cookie fields
// takes the whole input as the JSON body.
func ExamplePost() {
	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"})
	swaggerkit.Post(api, "/hives", func(ctx context.Context, in NewHive) (Hive, error) {
		return Hive{ID: 2, Name: in.Name}, nil
	}, swaggerkit.Status(http.StatusCreated))

	req := httptest.NewRequest(http.MethodPost, "/hives", strings.NewReader(`{"name":"Clover"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	fmt.Print(rec.Code, " ", rec.Body.String())
	// Output: 201 {"id":2,"name":"Clover","bees":0}
}

func ExampleAPI_WriteOpenAPI() {
	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"})
	swaggerkit.Get(api, "/hives/{hiveId}", GetHive)

	f, err := os.CreateTemp("", "openapi-*.json")
	if err != nil {
		panic(err)
	}
	defer os.Remove(f.Name())
	if err := api.WriteOpenAPI(f, swaggerkit.OpenAPI31); err != nil {
		panic(err)
	}
	fmt.Println(f.Close() == nil)
	// Output: true
}

func ExampleAPI_Lint() {
	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"})
	swaggerkit.Post(api, "/hives/delete/{hiveId}", func(ctx context.Context, in GetHiveInput) (swaggerkit.NoContent, error) {
		return swaggerkit.NoContent{}, nil
	})
	for _, issue := range api.Lint() {
		fmt.Println(issue)
	}
	// Output:
	// warning: POST /hives/delete/{hiveId}: path segment "delete" is a verb; express the action with the HTTP method, e.g. DELETE /projects/{id} (verb-in-path)
	// info: POST /hives/delete/{hiveId}: add Tags to group the operation in the documentation (operation-tags)
}

func ExampleBearerAuth() {
	verify := func(ctx context.Context, token string) (context.Context, error) {
		if token != "beekeeper" {
			return nil, swaggerkit.Unauthorized("unknown token")
		}
		return ctx, nil
	}
	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"},
		swaggerkit.WithSecurityScheme("beekeeper", swaggerkit.BearerAuth(verify)))
	keeper := api.Group("/hives", swaggerkit.Security("beekeeper"))
	swaggerkit.Delete(keeper, "/{hiveId}", func(ctx context.Context, in GetHiveInput) (swaggerkit.NoContent, error) {
		return swaggerkit.NoContent{}, nil
	})

	req := httptest.NewRequest(http.MethodDelete, "/hives/1", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	fmt.Println(rec.Code)

	req.Header.Set("Authorization", "Bearer beekeeper")
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	fmt.Println(rec.Code)
	// Output:
	// 401
	// 204
}
