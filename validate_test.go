package swaggerkit

import (
	"reflect"
	"strings"
	"testing"
)

type validated struct {
	Name    string            `json:"name" validate:"min=2,max=5"`
	Count   int32             `json:"count,omitempty" validate:"gt=0"`
	Ratio   float64           `json:"ratio,omitempty" validate:"multipleOf=0.5,lte=10"`
	Tags    []string          `json:"tags,omitempty" validate:"max=2,unique"`
	Kind    color             `json:"kind,omitempty"`
	Email   string            `json:"email,omitempty" validate:"email"`
	Child   *validatedChild   `json:"child,omitempty"`
	Labels  map[string]int    `json:"labels,omitempty" validate:"dive,max=3"`
	Nothing any               `json:"nothing,omitempty"`
	Level   int               `json:"level" default:"3"`
	Nested  []validatedChild  `json:"nested,omitempty"`
	Extra   map[string]string `json:"extra,omitempty"`
	Maybe   *int              `json:"maybe"`
}

type validatedChild struct {
	Size uint8 `json:"size"`
}

func TestValidator(t *testing.T) {
	g := newSchemaGen()
	s := g.schemaOf(reflect.TypeFor[validated](), "body")
	tests := []struct {
		body string
		want string // "" means valid; otherwise "location: message" substring
	}{
		{`{"name":"Bee","maybe":null}`, ""},
		{`{"name":"Бжж","maybe":1}`, ""}, // length counts characters, not bytes
		{`{"maybe":null}`, "body.name: is required"},
		{`{"name":"Bee"}`, ""}, // pointers are optional
		{`{"name":"B","maybe":null}`, "body.name: must be at least 2 characters long"},
		{`{"name":"Beehive","maybe":null}`, "body.name: must be at most 5 characters long"},
		{`{"name":5,"maybe":null}`, "body.name: must be a string"},
		{`{"name":null,"maybe":null}`, "body.name: must not be null"},
		{`{"name":"Bee","count":0,"maybe":null}`, "body.count: must be greater than 0"},
		{`{"name":"Bee","count":1.5,"maybe":null}`, "body.count: must be an integer"},
		{`{"name":"Bee","count":1e3,"maybe":null}`, "written without a fraction"},
		{`{"name":"Bee","count":3000000000,"maybe":null}`, "body.count: must fit into 32 bits"},
		{`{"name":"Bee","ratio":0.7,"maybe":null}`, "body.ratio: must be a multiple of 0.5"},
		{`{"name":"Bee","ratio":11,"maybe":null}`, "body.ratio: must be less than or equal to 10"},
		{`{"name":"Bee","tags":["a","a"],"maybe":null}`, "body.tags: must not contain duplicate items"},
		{`{"name":"Bee","tags":["a","b","c"],"maybe":null}`, "body.tags: must contain at most 2 items"},
		{`{"name":"Bee","tags":"a","maybe":null}`, "body.tags: must be an array"},
		{`{"name":"Bee","kind":"blue","maybe":null}`, `body.kind: must be one of ["red","green"]`},
		{`{"name":"Bee","email":"bee@hive","maybe":null}`, "body.email: must be a valid email address"},
		{`{"name":"Bee","child":{"size":300},"maybe":null}`, "body.child.size: must be less than or equal to 255"},
		{`{"name":"Bee","child":{},"maybe":null}`, "body.child.size: is required"},
		{`{"name":"Bee","child":[],"maybe":null}`, "body.child: must be an object"},
		{`{"name":"Bee","nested":[{"size":1},{"size":-1}],"maybe":null}`, "body.nested[1].size: must be greater than or equal to 0"},
		{`{"name":"Bee","labels":{"a":4},"maybe":null}`, "body.labels.a: must be less than or equal to 3"},
		{`{"name":"Bee","nothing":{"any":[1]},"maybe":null}`, ""},
		{`{"name":"Bee","extra":{"k":1},"maybe":null}`, "body.extra.k: must be a string"},
		{`{"name":"Bee","maybe":null,"color":"red"}`, "body.color: unknown field"},
		{`{"name":"Bee","maybe":null,"child":{"size":1,"x":1}}`, "body.child.x: unknown field"},
		{`{"name":"Bee","maybe":"x"}`, "body.maybe: must be a number"},
		{`[]`, "body: must be an object"},
	}
	for _, tt := range tests {
		v, err := decodeJSONValue([]byte(tt.body))
		if err != nil {
			t.Fatal(err)
		}
		val := validator{gen: g, strict: true, fillDefaults: true}
		val.check(s, v, "body")
		var got []string
		for _, e := range val.errs {
			got = append(got, e.Location+": "+e.Message)
		}
		joined := strings.Join(got, "; ")
		if tt.want == "" && joined != "" || !strings.Contains(joined, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.body, joined, tt.want)
		}
	}
}

func TestValidatorDefaultsAndLenientMode(t *testing.T) {
	g := newSchemaGen()
	s := g.schemaOf(reflect.TypeFor[validated](), "body")
	v, _ := decodeJSONValue([]byte(`{"name":"Bee","maybe":null,"unknown":1}`))
	val := validator{gen: g, strict: false, fillDefaults: true}
	val.check(s, v, "body")
	if len(val.errs) != 0 {
		t.Fatalf("errors: %v", val.errs)
	}
	if !val.changed {
		t.Fatal("default was not filled in")
	}
	if got := canonical(v.(map[string]any)["level"]); got != "n:3" {
		t.Fatalf("level = %s", got)
	}
}

func TestCheckFormat(t *testing.T) {
	tests := []struct {
		format, value string
		ok            bool
	}{
		{"email", "queen@hive.example", true},
		{"email", "Queen <queen@hive.example>", false},
		{"email", "queen@hive", false},
		{"email", "@hive.example", false},
		{"email", "queen@-hive.example", false},
		{"uri", "https://hive.example/a?b=1", true},
		{"uri", "/relative", false},
		{"uri", "mailto:queen@hive.example", true},
		{"uuid", "123e4567-e89b-12d3-a456-426614174000", true},
		{"uuid", "123e4567e89b12d3a456426614174000", false},
		{"ipv4", "192.168.0.1", true},
		{"ipv4", "::1", false},
		{"ipv6", "::1", true},
		{"ipv6", "192.168.0.1", false},
		{"hostname", "hive.example.", true},
		{"hostname", "hive_1.example", false},
		{"date", "2026-09-26", true},
		{"date", "2026-13-01", false},
		{"date-time", "2026-09-26T10:00:00Z", true},
		{"date-time", "2026-09-26 10:00", false},
		{"byte", "aGl2ZQ==", true},
		{"byte", "hive!", false},
		{"unknown-format", "anything", true},
	}
	for _, tt := range tests {
		msg := checkFormat(tt.format, tt.value)
		if (msg == "") != tt.ok {
			t.Errorf("checkFormat(%q, %q) = %q, want ok=%v", tt.format, tt.value, msg, tt.ok)
		}
	}
}
