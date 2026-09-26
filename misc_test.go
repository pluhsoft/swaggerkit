package swaggerkit

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPatchAndSummary(t *testing.T) {
	api, _ := newTestAPI()
	type in struct {
		ID   int `path:"id"`
		Body struct {
			Name string `json:"name"`
		}
	}
	Patch(api, "/hives/{id}", func(ctx context.Context, in in) (string, error) { return in.Body.Name, nil }, Summary("Rename"))
	expect(t, do(t, api, request{method: "PATCH", target: "/hives/1", body: `{"name":"Linden"}`}), 200, "Linden")
	if got := api.handlerName(t, "/hives/{id}"); !strings.HasSuffix(got, "|Rename") {
		t.Errorf("summary = %s", got)
	}
}

func TestRequestLoggerKeepsResponseController(t *testing.T) {
	var flushed bool
	h := RequestLogger(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.WriteHeader(http.StatusOK) // superfluous, ignored
		flushed = http.NewResponseController(w).Flush() == nil
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !flushed || rec.Code != http.StatusAccepted {
		t.Errorf("flushed=%v code=%d", flushed, rec.Code)
	}
}

func TestSecuritySchemeDocuments(t *testing.T) {
	tests := []struct {
		scheme SecurityScheme
		want   string
	}{
		{BearerAuth(nil), `{"type":"http","scheme":"bearer"}`},
		{func() SecurityScheme { s := BearerAuth(nil); s.BearerFormat, s.Description = "JWT", "d"; return s }(),
			`{"type":"http","description":"d","scheme":"bearer","bearerFormat":"JWT"}`},
		{BasicAuth(nil), `{"type":"http","scheme":"basic"}`},
		{APIKeyAuth("cookie", "sid", nil), `{"type":"apiKey","in":"cookie","name":"sid"}`},
		{OpenIDConnectAuth("https://id.example", nil), `{"type":"openIdConnect","openIdConnectUrl":"https://id.example"}`},
	}
	for _, tt := range tests {
		b, _ := marshalJSON(tt.scheme)
		if string(b) != tt.want {
			t.Errorf("got %s, want %s", b, tt.want)
		}
	}
}

func TestParseTagValue(t *testing.T) {
	tests := []struct {
		typ, raw, want string
		ok             bool
	}{
		{"boolean", "true", "true", true},
		{"boolean", "yes", "", false},
		{"number", "1.5", "1.5", true},
		{"number", "x", "", false},
		{"array", `["a"]`, `["a"]`, true},
		{"object", `{`, "", false},
		{"", `{"a":1}`, `{"a":1}`, true},
	}
	for _, tt := range tests {
		v, err := parseTagValue(&Schema{Type: tt.typ}, tt.raw)
		if (err == nil) != tt.ok {
			t.Errorf("%s %q: err = %v", tt.typ, tt.raw, err)
			continue
		}
		if tt.ok {
			if b, _ := marshalJSON(v); string(b) != tt.want {
				t.Errorf("%s %q = %s", tt.typ, tt.raw, b)
			}
		}
	}
	if v, _ := parseTagValue(nil, "x"); v != "x" {
		t.Error("nil schema")
	}
}

func TestComponentNameSuffix(t *testing.T) {
	g := newSchemaGen()
	g.taken["Hive"] = reflect.TypeFor[int]()
	g.taken["SwaggerkitHive"] = reflect.TypeFor[string]()
	type Hive struct{}
	if got := g.nameFor(reflect.TypeFor[Hive]()); got != "Hive2" {
		t.Errorf("name = %s", got)
	}
}

func TestInputErrors(t *testing.T) {
	type unexported struct {
		id int `path:"id"`
	}
	type twoBodies struct {
		Body  string
		Inner struct{ Body string }
		Embed
	}
	type pointerEmbed struct {
		*Embed
	}
	type noName struct {
		ID int `query:""`
	}
	type optionalPath struct {
		ID int `path:"id" validate:"optional"`
	}
	tests := []struct {
		typ  reflect.Type
		path []string
		want string
	}{
		{reflect.TypeFor[unexported](), []string{"id"}, "must be exported"},
		{reflect.TypeFor[twoBodies](), nil, "more than one Body"},
		{reflect.TypeFor[pointerEmbed](), nil, "embed parameter structs by value"},
		{reflect.TypeFor[noName](), nil, "tag needs a name"},
		{reflect.TypeFor[optionalPath](), []string{"id"}, "always required"},
	}
	for _, tt := range tests {
		func() {
			defer func() {
				e, ok := recover().(*schemaError)
				if !ok || !strings.Contains(e.Error(), tt.want) {
					t.Errorf("%s: got %v, want %q", tt.typ, e, tt.want)
				}
			}()
			analyzeInput(newSchemaGen(), tt.typ, http.MethodPost, tt.path, "test")
		}()
	}
	_ = unexported{}.id
}

type Embed struct {
	Body string
}

func TestContextHelpersOutsideHandlers(t *testing.T) {
	ctx := context.Background()
	if Request(ctx) != nil || ResponseHeader(ctx) == nil {
		t.Error("helpers must be safe outside handlers")
	}
}

func TestJSONHelpers(t *testing.T) {
	if !isJSONContentType("application/json; charset=utf-8") || isJSONContentType("text/json;;") || isJSONContentType("application/xml") {
		t.Error("isJSONContentType")
	}
	if typeWord(&Schema{Type: "number"}) != "a number" || typeWord(&Schema{}) != "a string" {
		t.Error("typeWord")
	}
	if got := jsonType(reflect.Float32); got != "number" {
		t.Error("jsonType")
	}
	if (&Error{Status: 499}).title() != "Error" {
		t.Error("title fallback")
	}
}

// The documentation site must load the same Swagger UI as WithDocs.
func TestDemoPageMatchesSwaggerUIVersion(t *testing.T) {
	page, err := os.ReadFile("docs/demo/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{swaggerUICSS, swaggerUICSSSRI, swaggerUIJS, swaggerUIJSSRI} {
		if !strings.Contains(string(page), want) {
			t.Errorf("docs/demo/index.html does not contain %s", want)
		}
	}
}
