package swaggerkit

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
)

// Schema is a JSON Schema used for request validation and in the OpenAPI document.
//
// Schemas are generated from Go types. Implement [SchemaProvider] to describe
// a type by hand.
type Schema struct {
	Ref         string `json:"$ref,omitempty"` // name of a component schema, e.g. "Hive"
	Type        string `json:"type,omitempty"` // "string", "integer", "number", "boolean", "array", "object"; empty means any value
	Format      string `json:"format,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Nullable    bool   `json:"-"` // rendered as type ["T", "null"]
	Deprecated  bool   `json:"deprecated,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
	Default     any    `json:"default,omitempty"`
	Examples    []any  `json:"examples,omitempty"`

	Minimum          *float64 `json:"minimum,omitempty"`
	Maximum          *float64 `json:"maximum,omitempty"`
	ExclusiveMinimum *float64 `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum *float64 `json:"exclusiveMaximum,omitempty"`
	MultipleOf       *float64 `json:"multipleOf,omitempty"`

	MinLength *int   `json:"minLength,omitempty"`
	MaxLength *int   `json:"maxLength,omitempty"`
	Pattern   string `json:"pattern,omitempty"`

	Items       *Schema `json:"items,omitempty"`
	MinItems    *int    `json:"minItems,omitempty"`
	MaxItems    *int    `json:"maxItems,omitempty"`
	UniqueItems bool    `json:"uniqueItems,omitempty"`

	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	MinProperties        *int               `json:"minProperties,omitempty"`
	MaxProperties        *int               `json:"maxProperties,omitempty"`

	pattern *regexp.Regexp
	closed  bool // generated from a Go struct: unknown properties are rejected in strict mode
}

// SchemaProvider is implemented by types that describe their own JSON Schema,
// for example types with a custom MarshalJSON.
type SchemaProvider interface {
	JSONSchema() *Schema
}

// MarshalJSON renders the schema as OpenAPI 3.1 expects it: references point
// to #/components/schemas and nullable schemas allow null.
func (s *Schema) MarshalJSON() ([]byte, error) {
	type plain Schema // without this method
	out := struct {
		Ref   string `json:"$ref,omitempty"`
		Type  any    `json:"type,omitempty"`
		Enum  []any  `json:"enum,omitempty"`
		AnyOf []any  `json:"anyOf,omitempty"`
		*plain
	}{Enum: s.Enum, plain: (*plain)(s)}
	if s.Type != "" {
		out.Type = s.Type
	}
	switch {
	case s.Ref != "" && s.Nullable:
		out.AnyOf = []any{map[string]string{"$ref": refPrefix + s.Ref}, map[string]string{"type": "null"}}
	case s.Ref != "":
		out.Ref = refPrefix + s.Ref
	case s.Nullable && s.Type != "":
		out.Type = []string{s.Type, "null"}
		if len(s.Enum) > 0 && !slices.Contains(s.Enum, nil) {
			out.Enum = append(slices.Clone(s.Enum), nil)
		}
	}
	return marshalJSON(out)
}

const refPrefix = "#/components/schemas/"

func (s *Schema) clone() *Schema {
	if s == nil {
		return nil
	}
	c := *s
	c.Enum = slices.Clone(s.Enum)
	c.Examples = slices.Clone(s.Examples)
	c.Required = slices.Clone(s.Required)
	c.Items = s.Items.clone()
	c.AdditionalProperties = s.AdditionalProperties.clone()
	if s.Properties != nil {
		c.Properties = make(map[string]*Schema, len(s.Properties))
		for k, v := range s.Properties {
			c.Properties[k] = v.clone()
		}
	}
	return &c
}

// propertyNames returns the property names in a stable order.
func (s *Schema) propertyNames() []string {
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

func (s *Schema) addRequired(name string) {
	if !slices.Contains(s.Required, name) {
		s.Required = append(s.Required, name)
	}
}

// marshalJSON encodes v without HTML escaping and without a trailing newline.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}
