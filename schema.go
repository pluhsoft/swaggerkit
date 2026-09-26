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
	Ref         string // name of a component schema, e.g. "Hive"
	Type        string // "string", "integer", "number", "boolean", "array", "object"; empty means any value
	Format      string
	Title       string
	Description string
	Nullable    bool
	Deprecated  bool
	Enum        []any
	Default     any
	Examples    []any

	Minimum          *float64
	Maximum          *float64
	ExclusiveMinimum *float64
	ExclusiveMaximum *float64
	MultipleOf       *float64

	MinLength *int
	MaxLength *int
	Pattern   string

	Items       *Schema
	MinItems    *int
	MaxItems    *int
	UniqueItems bool

	Properties           map[string]*Schema
	PropertyOrder        []string // order of Properties in the document; missing names are appended sorted
	Required             []string
	AdditionalProperties *Schema
	MinProperties        *int
	MaxProperties        *int

	pattern *regexp.Regexp
	closed  bool // generated from a Go struct: unknown properties are rejected in strict mode
}

// SchemaProvider is implemented by types that describe their own JSON Schema,
// for example types with a custom MarshalJSON.
type SchemaProvider interface {
	JSONSchema() *Schema
}

func (s *Schema) clone() *Schema {
	if s == nil {
		return nil
	}
	c := *s
	c.Enum = slices.Clone(s.Enum)
	c.Examples = slices.Clone(s.Examples)
	c.Required = slices.Clone(s.Required)
	c.PropertyOrder = slices.Clone(s.PropertyOrder)
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

func (s *Schema) propertyNames() []string {
	names := make([]string, 0, len(s.Properties))
	for _, n := range s.PropertyOrder {
		if _, ok := s.Properties[n]; ok && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	var rest []string
	for n := range s.Properties {
		if !slices.Contains(names, n) {
			rest = append(rest, n)
		}
	}
	slices.Sort(rest)
	return append(names, rest...)
}

func (s *Schema) addRequired(name string) {
	if !slices.Contains(s.Required, name) {
		s.Required = append(s.Required, name)
	}
}

// document renders the schema for the given OpenAPI version.
func (s *Schema) document(v OpenAPIVersion) orderedMap {
	var m orderedMap
	if s.Ref != "" {
		ref := orderedMap{{"$ref", "#/components/schemas/" + s.Ref}}
		switch {
		case s.Nullable && v == OpenAPI30:
			m.set("allOf", []any{ref})
			m.set("nullable", true)
		case s.Nullable:
			m.set("anyOf", []any{ref, orderedMap{{"type", "null"}}})
		case s.Description != "" || s.Deprecated || s.Default != nil || len(s.Examples) > 0:
			if v == OpenAPI30 {
				m.set("allOf", []any{ref}) // siblings of $ref are ignored in 3.0
			} else {
				m = ref
			}
		default:
			return ref
		}
		s.documentAnnotations(&m, v)
		return m
	}

	if s.Type != "" {
		if s.Nullable && v == OpenAPI31 {
			m.set("type", []string{s.Type, "null"})
		} else {
			m.set("type", s.Type)
		}
	}
	if s.Nullable && v == OpenAPI30 {
		m.set("nullable", true)
	}
	if s.Format != "" {
		m.set("format", s.Format)
	}
	if s.Title != "" {
		m.set("title", s.Title)
	}
	if len(s.Enum) > 0 {
		enum := s.Enum
		if s.Nullable && !slices.Contains(enum, nil) {
			enum = append(slices.Clone(enum), nil)
		}
		m.set("enum", enum)
	}
	if s.MultipleOf != nil {
		m.set("multipleOf", *s.MultipleOf)
	}
	setNum := func(key string, p *float64) {
		if p != nil {
			m.set(key, *p)
		}
	}
	setNum("minimum", s.Minimum)
	if s.ExclusiveMinimum != nil {
		if v == OpenAPI30 {
			m.set("minimum", *s.ExclusiveMinimum)
			m.set("exclusiveMinimum", true)
		} else {
			m.set("exclusiveMinimum", *s.ExclusiveMinimum)
		}
	}
	setNum("maximum", s.Maximum)
	if s.ExclusiveMaximum != nil {
		if v == OpenAPI30 {
			m.set("maximum", *s.ExclusiveMaximum)
			m.set("exclusiveMaximum", true)
		} else {
			m.set("exclusiveMaximum", *s.ExclusiveMaximum)
		}
	}
	setInt := func(key string, p *int) {
		if p != nil {
			m.set(key, *p)
		}
	}
	setInt("minLength", s.MinLength)
	setInt("maxLength", s.MaxLength)
	if s.Pattern != "" {
		m.set("pattern", s.Pattern)
	}
	if s.Items != nil {
		m.set("items", s.Items.document(v))
	}
	setInt("minItems", s.MinItems)
	setInt("maxItems", s.MaxItems)
	if s.UniqueItems {
		m.set("uniqueItems", true)
	}
	if len(s.Properties) > 0 {
		var props orderedMap
		for _, name := range s.propertyNames() {
			props.set(name, s.Properties[name].document(v))
		}
		m.set("properties", props)
	}
	if len(s.Required) > 0 {
		m.set("required", s.Required)
	}
	if s.AdditionalProperties != nil {
		m.set("additionalProperties", s.AdditionalProperties.document(v))
	}
	setInt("minProperties", s.MinProperties)
	setInt("maxProperties", s.MaxProperties)
	s.documentAnnotations(&m, v)
	if m == nil {
		return orderedMap{}
	}
	return m
}

func (s *Schema) documentAnnotations(m *orderedMap, v OpenAPIVersion) {
	if s.Description != "" {
		m.set("description", s.Description)
	}
	if s.Default != nil {
		m.set("default", s.Default)
	}
	if len(s.Examples) > 0 {
		if v == OpenAPI30 {
			m.set("example", s.Examples[0])
		} else {
			m.set("examples", s.Examples)
		}
	}
	if s.Deprecated {
		m.set("deprecated", true)
	}
}

// orderedMap is a JSON object that keeps key order.
type orderedMap []keyValue

type keyValue struct {
	Key   string
	Value any
}

func (m *orderedMap) set(key string, value any) {
	for i := range *m {
		if (*m)[i].Key == key {
			(*m)[i].Value = value
			return
		}
	}
	*m = append(*m, keyValue{key, value})
}

func (m orderedMap) MarshalJSON() ([]byte, error) {
	buf := []byte{'{'}
	for i, kv := range m {
		if i > 0 {
			buf = append(buf, ',')
		}
		k, err := marshalJSON(kv.Key)
		if err != nil {
			return nil, err
		}
		v, err := marshalJSON(kv.Value)
		if err != nil {
			return nil, err
		}
		buf = append(buf, k...)
		buf = append(buf, ':')
		buf = append(buf, v...)
	}
	return append(buf, '}'), nil
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
