package swaggerkit

import (
	"encoding/json"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

// docJSON renders a schema and its components for comparison.
func docJSON(t *testing.T, s *Schema) string {
	t.Helper()
	b, err := marshalJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func componentJSON(t *testing.T, g *schemaGen, name string) string {
	t.Helper()
	c, ok := g.components[name]
	if !ok {
		t.Fatalf("no component %s; have %v", name, g.componentsSorted())
	}
	return docJSON(t, c)
}

func TestSchemaOfBasicTypes(t *testing.T) {
	var (
		anyValue any
		mapValue map[string]int
	)
	tests := []struct {
		value any
		want  string
	}{
		{true, `{"type":"boolean"}`},
		{int8(0), `{"type":"integer","format":"int32","minimum":-128,"maximum":127}`},
		{int32(0), `{"type":"integer","format":"int32"}`},
		{0, `{"type":"integer","format":"int64"}`},
		{uint8(0), `{"type":"integer","format":"int32","minimum":0,"maximum":255}`},
		{uint(0), `{"type":"integer","minimum":0}`},
		{float32(0), `{"type":"number","format":"float"}`},
		{0.0, `{"type":"number","format":"double"}`},
		{"", `{"type":"string"}`},
		{[]byte(nil), `{"type":"string","format":"byte"}`},
		{[]string(nil), `{"type":"array","items":{"type":"string"}}`},
		{[2]int{}, `{"type":"array","items":{"type":"integer","format":"int64"},"minItems":2,"maxItems":2}`},
		{mapValue, `{"type":"object","additionalProperties":{"type":"integer","format":"int64"}}`},
		{time.Time{}, `{"type":"string","format":"date-time"}`},
		{json.RawMessage(nil), `{}`},
		{net.IP(nil), `{"type":"string"}`},
		{&anyValue, `{}`},
		{struct {
			A int `json:"a"`
		}{}, `{"type":"object","properties":{"a":{"type":"integer","format":"int64"}},"required":["a"]}`},
	}
	for _, tt := range tests {
		g := newSchemaGen()
		got := docJSON(t, g.schemaOf(reflect.TypeOf(tt.value), "test"))
		if got != tt.want {
			t.Errorf("%T:\n got %s\nwant %s", tt.value, got, tt.want)
		}
	}
}

func TestSchemaOfUnsupportedType(t *testing.T) {
	defer func() {
		e, ok := recover().(*schemaError)
		if !ok || !strings.Contains(e.Error(), "cannot be encoded as JSON") {
			t.Fatalf("got %v", e)
		}
	}()
	newSchemaGen().schemaOf(reflect.TypeFor[chan int](), "test")
}

type embeddedBase struct {
	ID      int    `json:"id"`
	Created string `json:"created,omitempty"`
}

type tagged struct {
	embeddedBase
	Name     string  `json:"name"`
	Skipped  string  `json:"-"`
	Renamed  int     `json:"count,string"`
	Optional *string `json:"optional,omitempty"`
	Nullable *string `json:"nullable"`
	Plain    bool
	private  int
}

func TestSchemaJSONTags(t *testing.T) {
	g := newSchemaGen()
	s := g.schemaOf(reflect.TypeFor[tagged](), "test")
	if s.Ref != "tagged" {
		t.Fatalf("ref = %q", s.Ref)
	}
	want := `{"type":"object","properties":{"Plain":{"type":"boolean"},"count":{"type":"string"},"created":{"type":"string"},` +
		`"id":{"type":"integer","format":"int64"},"name":{"type":"string"},"nullable":{"type":["string","null"]},` +
		`"optional":{"type":"string"}},"required":["id","name","count","Plain"]}`
	if got := componentJSON(t, g, "tagged"); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
	_ = tagged{}.private
}

type conflictA struct {
	Name string
}

type conflictB struct {
	Name string
}

type conflicts struct {
	conflictA
	conflictB
	Outer string `json:"kept"`
}

func TestSchemaEmbeddedConflicts(t *testing.T) {
	g := newSchemaGen()
	g.schemaOf(reflect.TypeFor[conflicts](), "test")
	props := g.components["conflicts"].Properties
	if _, ok := props["Name"]; ok {
		t.Error("ambiguous embedded field must be dropped")
	}
	if _, ok := props["kept"]; !ok {
		t.Error("outer field must be kept")
	}
}

type node struct {
	Name     string `json:"name"`
	Children []node `json:"children,omitempty"`
	Parent   *node  `json:"parent,omitempty"`
}

func TestSchemaRecursiveType(t *testing.T) {
	g := newSchemaGen()
	g.schemaOf(reflect.TypeFor[node](), "test")
	want := `{"type":"object","properties":{"children":{"type":"array","items":{"$ref":"#/components/schemas/node"}},` +
		`"name":{"type":"string"},"parent":{"$ref":"#/components/schemas/node"}},"required":["name"]}`
	if got := componentJSON(t, g, "node"); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

type page[T any] struct {
	Items []T `json:"items"`
}

func TestSchemaComponentNames(t *testing.T) {
	type item struct {
		A int `json:"a"`
	}
	first := reflect.TypeFor[item]()
	g := newSchemaGen()
	g.schemaOf(reflect.TypeFor[page[item]](), "test")
	g.schemaOf(first, "test")
	func() {
		type item struct {
			B int `json:"b"`
		}
		g.schemaOf(reflect.TypeFor[item](), "test")
	}()
	got := strings.Join(g.componentsSorted(), ",")
	if got != "SwaggerkitItem,item,pageItem" {
		t.Errorf("components = %s", got)
	}
}

type color string

func (color) Enum() []color { return []color{"red", "green"} }

type level int

func (*level) Enum() []any { return []any{1, 2, 3} }

type point struct{ X, Y int }

func (point) JSONSchema() *Schema {
	return &Schema{Type: "string", Pattern: `^\d+,\d+$`, Description: "x,y"}
}

type customJSON struct{}

func (customJSON) MarshalJSON() ([]byte, error) { return []byte(`"x"`), nil }

func TestSchemaEnumsAndProviders(t *testing.T) {
	g := newSchemaGen()
	s := g.schemaOf(reflect.TypeFor[struct {
		Color  color       `json:"color"`
		Level  level       `json:"level"`
		Point  point       `json:"point"`
		Custom customJSON  `json:"custom"`
		Colors []color     `json:"colors"`
		Maybe  *color      `json:"maybe"`
		Tagged interface{} `json:"tagged"`
	}](), "test")
	got := docJSON(t, s)
	for _, want := range []string{
		`"color":{"$ref":"#/components/schemas/color"}`,
		`"colors":{"type":"array","items":{"$ref":"#/components/schemas/color"}}`,
		`"maybe":{"anyOf":[{"$ref":"#/components/schemas/color"},{"type":"null"}]}`,
		`"custom":{}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("schema %s\ndoes not contain %s", got, want)
		}
	}
	if got := componentJSON(t, g, "color"); got != `{"type":"string","enum":["red","green"]}` {
		t.Errorf("color = %s", got)
	}
	if got := componentJSON(t, g, "level"); got != `{"type":"integer","enum":[1,2,3]}` {
		t.Errorf("level = %s", got)
	}
	if got := componentJSON(t, g, "point"); got != `{"type":"string","description":"x,y","pattern":"^\\d+,\\d+$"}` {
		t.Errorf("point = %s", got)
	}
	if len(g.issues) != 2 {
		t.Errorf("want 2 untyped-value issues, got %v", g.issues)
	}
}

func TestSchemaTags(t *testing.T) {
	g := newSchemaGen()
	s := g.schemaOf(reflect.TypeFor[struct {
		Name   string            `json:"name" validate:"required,min=1,max=64" pattern:"^[a-z]+$" deprecated:"true"`
		Email  string            `json:"email,omitempty" validate:"email"`
		Size   int               `json:"size" validate:"optional,gte=1,lte=5,multipleOf=1" default:"3"`
		Kind   string            `json:"kind" validate:"oneof=a b" default:"a"`
		Tags   []string          `json:"tags" validate:"len=2,unique,dive,uuid"`
		Labels map[string]string `json:"labels" validate:"max=3,dive,max=10"`
	}](), "test")
	got := docJSON(t, s)
	want := `{"type":"object","properties":{` +
		`"email":{"type":"string","format":"email"},` +
		`"kind":{"type":"string","enum":["a","b"],"default":"a"},` +
		`"labels":{"type":"object","additionalProperties":{"type":"string","maxLength":10},"maxProperties":3},` +
		`"name":{"type":"string","deprecated":true,"minLength":1,"maxLength":64,"pattern":"^[a-z]+$"},` +
		`"size":{"type":"integer","format":"int64","default":3,"minimum":1,"maximum":5,"multipleOf":1},` +
		`"tags":{"type":"array","items":{"type":"string","format":"uuid"},"minItems":2,"maxItems":2,"uniqueItems":true}},` +
		`"required":["name","tags","labels"]}`
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestSchemaTagErrors(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"unknown rule", struct {
			A string `validate:"requird"`
		}{}, `unknown rule "requird"`},
		{"min on bool", struct {
			A bool `validate:"min=1"`
		}{}, "applies to numbers, strings"},
		{"gt on string", struct {
			A string `validate:"gt=1"`
		}{}, "applies to numbers only"},
		{"missing argument", struct {
			A int `validate:"min"`
		}{}, "needs an argument"},
		{"bad pattern", struct {
			A string `pattern:"("`
		}{}, "invalid pattern"},
		{"pattern on int", struct {
			A int `pattern:"1"`
		}{}, "string fields only"},
		{"bad default", struct {
			A int `default:"x"`
		}{}, "not an integer"},
		{"default breaks rule", struct {
			A int `validate:"max=5" default:"7"`
		}{}, "default \"7\" is invalid"},
		{"dive on scalar", struct {
			A string `validate:"dive,min=1"`
		}{}, "slices and maps only"},
		{"rule on component", struct {
			A node `validate:"min=1"`
		}{}, "cannot be applied to node"},
		{"required after dive", struct {
			A []string `validate:"dive,required"`
		}{}, "cannot follow dive"},
		{"email on int", struct {
			A int `validate:"email"`
		}{}, "strings only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				e, ok := recover().(*schemaError)
				if !ok || !strings.Contains(e.Error(), tt.want) {
					t.Fatalf("got %v, want %q", e, tt.want)
				}
			}()
			newSchemaGen().schemaOf(reflect.TypeOf(tt.value), "test")
		})
	}
}

func TestUnknownRuleMessage(t *testing.T) {
	_, err := applyRules(&Schema{Type: "string"}, "nope", newSchemaGen())
	if err == nil || !strings.Contains(err.Error(), "unknown rule") {
		t.Fatalf("err = %v", err)
	}
}

type documentedHive struct {
	Name string `json:"name"`
}

func (documentedHive) Doc() string { return "A beehive." }

type documentedStatus string

func (documentedStatus) Enum() []documentedStatus { return []documentedStatus{"active"} }
func (*documentedStatus) Doc() string             { return "Colony state." }

func (point) Doc() string { return "ignored: JSONSchema sets the description" }

func TestSchemaTypeDoc(t *testing.T) {
	g := newSchemaGen()
	g.schemaOf(reflect.TypeFor[documentedHive](), "test")
	g.schemaOf(reflect.TypeFor[documentedStatus](), "test")
	g.schemaOf(reflect.TypeFor[point](), "test")
	for name, want := range map[string]string{"documentedHive": "A beehive.", "documentedStatus": "Colony state.", "point": "x,y"} {
		if got := g.components[name].Description; got != want {
			t.Errorf("%s description = %q, want %q", name, got, want)
		}
	}
}
