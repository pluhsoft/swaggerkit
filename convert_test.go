package swaggerkit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDowngradeSchema(t *testing.T) {
	tests := []struct{ in, want string }{
		{`{"type":["string","null"]}`, `{"nullable":true,"type":"string"}`},
		{`{"anyOf":[{"$ref":"#/components/schemas/Hive"},{"type":"null"}]}`, `{"allOf":[{"$ref":"#/components/schemas/Hive"}],"nullable":true}`},
		{`{"$ref":"#/components/schemas/Hive","description":"d"}`, `{"allOf":[{"$ref":"#/components/schemas/Hive"}],"description":"d"}`},
		{`{"$ref":"#/components/schemas/Hive"}`, `{"$ref":"#/components/schemas/Hive"}`},
		{`{"type":"integer","examples":[1,2]}`, `{"example":1,"type":"integer"}`},
		{`{"type":"number","exclusiveMinimum":0,"exclusiveMaximum":10}`, `{"exclusiveMaximum":true,"exclusiveMinimum":true,"maximum":10,"minimum":0,"type":"number"}`},
	}
	for _, tt := range tests {
		var s map[string]any
		dec := json.NewDecoder(strings.NewReader(tt.in))
		dec.UseNumber()
		if err := dec.Decode(&s); err != nil {
			t.Fatal(err)
		}
		downgradeSchema(s, "nullable")
		if got, _ := marshalJSON(s); string(got) != tt.want {
			t.Errorf("%s:\n got %s\nwant %s", tt.in, got, tt.want)
		}
	}
}

func legacyAPI(t *testing.T) *API {
	t.Helper()
	api, _ := newTestAPI(
		WithServer("https://hive.example/", "prod"),
		WithBasePath("/api/v1"),
		WithSecurityScheme("jwt", BearerAuth("JWT")),
		WithSecurityScheme("basic", BasicAuth()),
		WithSecurityScheme("key", APIKeyAuth("header", "X-Key")),
		WithSecurityScheme("oidc", OpenIDConnectAuth("https://id.example")),
	)
	api.info.License = &License{Name: "MIT", Identifier: "MIT"}
	type in struct {
		ID      int      `path:"id"`
		Tags    []string `query:"tag"`
		Session string   `cookie:"session"`
		Body    *node
	}
	Put(api, "/hives/{id}", func(ctx context.Context, _ in) (validated, error) { return validated{}, nil },
		Security("jwt"), Middlewares(requireToken))
	Get(api, "/files/{path...}", func(ctx context.Context, _ struct {
		Path string `path:"path"`
	}) (*File, error) {
		return nil, nil
	}, Produces("image/png"))
	return api
}

func TestOpenAPI30(t *testing.T) {
	doc, err := legacyAPI(t).Document(FormatOpenAPI30)
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	if !strings.HasPrefix(text, "{\n  \"openapi\": \"3.0.3\"") {
		t.Errorf("version must come first:\n%.80s", text)
	}
	for _, bad := range []string{`"identifier"`, `"examples"`, `"null"`} {
		if strings.Contains(text, bad) {
			t.Errorf("3.0 document contains %s", bad)
		}
	}
	if !strings.Contains(text, `"nullable": true`) {
		t.Error("nullable pointer lost")
	}
}

func TestSwagger20(t *testing.T) {
	api := legacyAPI(t)
	raw, err := api.Document(FormatSwagger20)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	checks := map[string]any{
		"swagger":  "2.0",
		"host":     "hive.example",
		"basePath": "/api/v1",
	}
	for k, want := range checks {
		if doc[k] != want {
			t.Errorf("%s = %v, want %v", k, doc[k], want)
		}
	}
	if strings.Contains(text, "#/components/") || strings.Contains(text, `"components"`) {
		t.Error("references must point to #/definitions")
	}
	defs := obj(doc["securityDefinitions"])
	if obj(defs["basic"])["type"] != "basic" || obj(defs["key"])["name"] != "X-Key" ||
		obj(defs["jwt"])["name"] != "Authorization" || obj(defs["oidc"])["in"] != "header" {
		t.Errorf("securityDefinitions = %v", defs)
	}
	put := obj(obj(obj(doc["paths"])["/hives/{id}"])["put"])
	var names []string
	for _, p := range arr(put["parameters"]) {
		p := obj(p)
		names = append(names, str(p["in"])+":"+str(p["name"]))
		if p["name"] == "tag" && p["collectionFormat"] != "multi" {
			t.Error("query arrays need collectionFormat multi")
		}
	}
	if strings.Join(names, ",") != "path:id,query:tag,body:body" {
		t.Errorf("parameters = %v (cookies are dropped)", names)
	}
	file := obj(obj(obj(obj(doc["paths"])["/files/{path}"])["get"])["responses"])
	if obj(obj(file["200"])["schema"])["type"] != "file" {
		t.Errorf("file response = %v", file["200"])
	}

	// Documents are cached and rebuilt after new routes.
	again, _ := api.Document(FormatSwagger20)
	if &again[0] != &raw[0] {
		t.Error("document is not cached")
	}
	Get(api, "/late", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
	if again, _ = api.Document(FormatSwagger20); !strings.Contains(string(again), `"/late"`) {
		t.Error("document was not rebuilt")
	}
}

func TestSwagger20Forms(t *testing.T) {
	op := swaggerOperation(map[string]any{
		"requestBody": map[string]any{"content": map[string]any{"multipart/form-data": map[string]any{"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"photo":   map[string]any{"type": "file", "description": "JPEG"},
				"caption": map[string]any{"type": "string"},
				"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []any{"photo"},
		}}}},
		"responses": map[string]any{"200": map[string]any{"description": "OK", "headers": map[string]any{
			"X-Total-Count": map[string]any{"description": "Total", "schema": map[string]any{"type": "string"}},
		}}},
	})
	got, _ := marshalJSON(op)
	for _, want := range []string{
		`"consumes":["multipart/form-data"]`,
		`{"description":"JPEG","in":"formData","name":"photo","required":true,"type":"file"}`,
		`{"collectionFormat":"multi","in":"formData","items":{"type":"string"},"name":"tags","type":"array"}`,
		`"headers":{"X-Total-Count":{"description":"Total","type":"string"}}`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("operation %s\ndoes not contain %s", got, want)
		}
	}
}

func TestDocumentEndpoints(t *testing.T) {
	api, _ := newTestAPI(WithDocs("/docs"))
	expect(t, do(t, api, request{method: "GET", target: "/docs/openapi-3.0.json"}), 200, `"openapi": "3.0.3"`)
	expect(t, do(t, api, request{method: "GET", target: "/docs/swagger.json"}), 200, `"swagger": "2.0"`)
	expect(t, do(t, api.DocumentHandler("yaml"), request{method: "GET", target: "/"}), 500)
	if doc, _ := api.Document(""); !strings.Contains(string(doc), `"openapi": "3.1.0"`) {
		t.Error("empty format must be OpenAPI 3.1")
	}
}
