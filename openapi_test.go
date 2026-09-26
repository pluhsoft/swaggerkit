package swaggerkit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type Problem struct {
	Code int `json:"code"`
}

func TestOpenAPIDocument(t *testing.T) {
	api, _ := newTestAPI(
		WithSecurityScheme("jwt", BearerAuth(nil)),
		WithTag("hives", "Beehives"),
	)
	api.info.Description = "Bees"
	api.info.Contact = &Contact{Name: "Keeper", Email: "keeper@hive.example"}
	api.info.License = &License{Name: "MIT", Identifier: "MIT"}
	type in struct {
		ID   int    `path:"id" doc:"Hive" deprecated:"true"`
		Body *hive  `doc:"New state"`
		Q    string `query:"q"`
	}
	Put(api, "/hives/{id}", func(ctx context.Context, _ in) (Problem, error) { return Problem{}, nil },
		Tags("hives", "extra"), Security("jwt"), Errors(404, 404), Deprecated(), Description("Replace a hive"))
	Get(api, "/hidden", func(ctx context.Context, _ struct{}) (string, error) { return "", nil }, Hidden())
	Get(api, "/files/{path...}", func(ctx context.Context, _ struct {
		Path string `path:"path"`
	}) (*File, error) {
		return nil, nil
	}, Produces("image/png"))

	raw, err := api.OpenAPI(OpenAPI31)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	get := func(path ...string) any {
		var v any = doc
		for _, p := range path {
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[p]
		}
		return v
	}
	checks := []struct {
		path []string
		want any
	}{
		{[]string{"openapi"}, "3.1.0"},
		{[]string{"info", "license", "identifier"}, "MIT"},
		{[]string{"info", "contact", "email"}, "keeper@hive.example"},
		{[]string{"paths", "/hives/{id}", "put", "operationId"}, "putHivesId"},
		{[]string{"paths", "/hives/{id}", "put", "deprecated"}, true},
		{[]string{"paths", "/hives/{id}", "put", "description"}, "Replace a hive"},
		{[]string{"paths", "/hives/{id}", "put", "requestBody", "description"}, "New state"},
		{[]string{"paths", "/hives/{id}", "put", "responses", "404", "description"}, "Not Found"},
		{[]string{"paths", "/hives/{id}", "put", "responses", "401", "description"}, "Unauthorized"},
		{[]string{"paths", "/hives/{id}", "put", "responses", "200", "content", "application/json", "schema", "$ref"}, "#/components/schemas/Problem"},
		{[]string{"paths", "/files/{path}", "get", "responses", "200", "content", "image/png", "schema", "format"}, "binary"},
		{[]string{"components", "schemas", "Problem2", "description"}, "Problem details, RFC 9457."},
		{[]string{"components", "securitySchemes", "jwt", "scheme"}, "bearer"},
	}
	for _, c := range checks {
		if got := get(c.path...); got != c.want {
			t.Errorf("%v = %v, want %v", c.path, got, c.want)
		}
	}
	if _, ok := get("paths").(map[string]any)["/hidden"]; ok {
		t.Error("hidden route is documented")
	}
	params := get("paths", "/hives/{id}", "put", "parameters").([]any)
	id := params[0].(map[string]any)
	if id["required"] != true || id["deprecated"] != true || id["description"] != "Hive" {
		t.Errorf("id parameter = %v", id)
	}
	if body := get("paths", "/hives/{id}", "put", "requestBody").(map[string]any); body["required"] != nil {
		t.Error("pointer body must be optional")
	}
	tags := get("tags").([]any)
	if len(tags) != 2 || tags[0].(map[string]any)["description"] != "Beehives" || tags[1].(map[string]any)["name"] != "extra" {
		t.Errorf("tags = %v", tags)
	}

	v30, err := api.OpenAPI(OpenAPI30)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(v30, []byte(`"openapi": "3.0.3"`)) || bytes.Contains(v30, []byte(`"identifier"`)) {
		t.Error("3.0 document is wrong")
	}

	// New routes invalidate the cached document.
	Get(api, "/late", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
	var buf bytes.Buffer
	if err := api.WriteOpenAPI(&buf, OpenAPI31); err != nil || !strings.Contains(buf.String(), `"/late"`) {
		t.Errorf("document was not rebuilt: %v", err)
	}
}

func TestOpenAPIHandler(t *testing.T) {
	api, _ := newTestAPI()
	rec := do(t, api.OpenAPIHandler(OpenAPI30), request{method: "GET", target: "/"})
	expect(t, rec, 200, `"openapi": "3.0.3"`, `"paths": {}`)
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("wrong content type")
	}
}

func TestLint(t *testing.T) {
	api := New(Info{}, WithSecurityScheme("key", APIKeyAuth("query", "key", nil)))
	type none struct{}
	type idIn struct {
		ID int `path:"Project_ID"`
	}
	type bodyIn struct {
		ID   int `path:"id"`
		Body struct {
			Name  string            `json:"name"`
			Tags  []string          `json:"tags" validate:"max=3"`
			Items []int             `json:"items"`
			Meta  map[string]string `json:"meta"`
			Any   any               `json:"any"`
		}
	}
	handler := func(ctx context.Context, _ none) ([]string, error) { return nil, nil }
	Post(api, "/project/delete/{Project_ID}", func(ctx context.Context, _ idIn) (string, error) { return "", nil })
	Post(api, "/Hives", handler)
	Delete(api, "/hives/{id}", func(ctx context.Context, _ bodyIn) (string, error) { return "", nil }, Security("key"))
	Get(api, "/list", handler, Tags("t"))

	var got []string
	for _, issue := range api.Lint() {
		got = append(got, issue.Rule+" "+issue.Location)
		if issue.String() == "" {
			t.Error("empty issue text")
		}
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{
		"info-title info",
		"info-version info",
		"security-not-enforced securitySchemes.key",
		"api-key-in-query securitySchemes.key",
		"path-param-case POST /project/delete/{Project_ID}",
		"verb-in-path POST /project/delete/{Project_ID}",
		"path-case POST /Hives",
		"operation-tags POST /Hives",
		"post-status POST /Hives",
		"unsecured-write POST /Hives",
		"array-response GET /list",
		"delete-body DELETE /hives/{id}",
		"unbounded-string DELETE /hives/{id}",
		"unbounded-array DELETE /hives/{id}",
		"unbounded-map DELETE /hives/{id}",
		"untyped-value body.Any",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing issue %q in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "unsecured-write DELETE") {
		t.Error("secured route reported as unsecured")
	}
	_ = http.MethodGet
}
