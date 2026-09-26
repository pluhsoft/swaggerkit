package swaggerkit

import (
	"encoding"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	timeType           = reflect.TypeFor[time.Time]()
	rawMessageType     = reflect.TypeFor[json.RawMessage]()
	jsonMarshalerType  = reflect.TypeFor[json.Marshaler]()
	textMarshalerType  = reflect.TypeFor[encoding.TextMarshaler]()
	schemaProviderType = reflect.TypeFor[SchemaProvider]()
)

// schemaGen builds schemas from Go types and collects named types as components.
type schemaGen struct {
	components map[string]*Schema
	names      map[reflect.Type]string
	taken      map[string]reflect.Type
	issues     []Issue // quality hints found while generating
}

func newSchemaGen() *schemaGen {
	return &schemaGen{
		components: map[string]*Schema{},
		names:      map[reflect.Type]string{},
		taken:      map[string]reflect.Type{},
	}
}

// schemaError is a programming error in a Go type, reported by panicking at registration.
type schemaError struct {
	where string
	msg   string
}

func (e *schemaError) Error() string { return "swaggerkit: " + e.where + ": " + e.msg }

func fail(where, format string, args ...any) {
	panic(&schemaError{where: where, msg: fmt.Sprintf(format, args...)})
}

// schemaOf returns a new schema for t. Named structs, enums and schema providers
// become components and are returned as references.
func (g *schemaGen) schemaOf(t reflect.Type, where string) *Schema {
	t = deref(t)
	if name, ok := g.names[t]; ok {
		return &Schema{Ref: name}
	}
	switch {
	case implements(t, schemaProviderType):
		s := providedSchema(t)
		if t.Name() == "" {
			return s
		}
		return g.component(t, func(*Schema) *Schema { return s })
	case t == timeType:
		return &Schema{Type: "string", Format: "date-time"}
	case t == rawMessageType:
		return &Schema{}
	}
	if values, ok := enumValues(t, where); ok {
		return g.component(t, func(*Schema) *Schema {
			return &Schema{Type: jsonType(t.Kind()), Enum: values}
		})
	}
	switch {
	case implements(t, jsonMarshalerType):
		g.untyped(where, "%s has a custom MarshalJSON; implement swaggerkit.SchemaProvider to document it", t)
		return &Schema{}
	case implements(t, textMarshalerType):
		return &Schema{Type: "string"}
	}

	switch t.Kind() {
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int8:
		return intSchema("int32", math.MinInt8, math.MaxInt8)
	case reflect.Int16:
		return intSchema("int32", math.MinInt16, math.MaxInt16)
	case reflect.Int32:
		return &Schema{Type: "integer", Format: "int32"}
	case reflect.Int, reflect.Int64:
		return &Schema{Type: "integer", Format: "int64"}
	case reflect.Uint8:
		return intSchema("int32", 0, math.MaxUint8)
	case reflect.Uint16:
		return intSchema("int32", 0, math.MaxUint16)
	case reflect.Uint32:
		return intSchema("int64", 0, math.MaxUint32)
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return &Schema{Type: "integer", Minimum: ptr(0.0)}
	case reflect.Float32:
		return &Schema{Type: "number", Format: "float"}
	case reflect.Float64:
		return &Schema{Type: "number", Format: "double"}
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && !implements(t.Elem(), textMarshalerType) {
			return &Schema{Type: "string", Format: "byte"} // encoding/json uses base64
		}
		return &Schema{Type: "array", Items: g.schemaOf(t.Elem(), where+"[]")}
	case reflect.Array:
		n := t.Len()
		return &Schema{Type: "array", Items: g.schemaOf(t.Elem(), where+"[]"), MinItems: &n, MaxItems: ptr(n)}
	case reflect.Map:
		switch t.Key().Kind() {
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		default:
			if !implements(t.Key(), textMarshalerType) {
				fail(where, "map key type %s is not supported by encoding/json", t.Key())
			}
		}
		return &Schema{Type: "object", AdditionalProperties: g.schemaOf(t.Elem(), where+"{}")}
	case reflect.Struct:
		if t.Name() == "" {
			s := &Schema{Type: "object", closed: true}
			g.fillObject(s, t, where)
			return s
		}
		return g.component(t, func(s *Schema) *Schema {
			s.Type, s.closed = "object", true
			g.fillObject(s, t, t.Name())
			return s
		})
	case reflect.Interface:
		g.untyped(where, "interface type %s accepts any value; use a concrete type", t)
		return &Schema{}
	}
	fail(where, "type %s cannot be encoded as JSON", t)
	return nil
}

// component registers t under a unique name. build receives the empty component
// schema, already registered, so recursive types resolve to references.
func (g *schemaGen) component(t reflect.Type, build func(*Schema) *Schema) *Schema {
	name := g.nameFor(t)
	g.names[t] = name
	s := &Schema{}
	g.components[name] = s
	if built := build(s); built != s {
		g.components[name] = built
	}
	return &Schema{Ref: name}
}

func (g *schemaGen) nameFor(t reflect.Type) string {
	base := componentName(t)
	candidates := []string{base, exportedName(path.Base(t.PkgPath())) + exportedName(base)}
	for _, c := range candidates {
		if _, ok := g.taken[c]; !ok {
			g.taken[c] = t
			return c
		}
	}
	for i := 2; ; i++ {
		c := base + strconv.Itoa(i)
		if _, ok := g.taken[c]; !ok {
			g.taken[c] = t
			return c
		}
	}
}

var (
	nonAlnum    = regexp.MustCompile(`[^A-Za-z0-9]+`)
	localSuffix = regexp.MustCompile(`·\d+`) // types declared inside functions
)

// componentName turns "Page[example.com/apiary.Hive]" into "PageHive".
func componentName(t reflect.Type) string {
	name := localSuffix.ReplaceAllString(t.Name(), "")
	base, args, generic := strings.Cut(name, "[")
	if !generic {
		return nonAlnum.ReplaceAllString(name, "")
	}
	var b strings.Builder
	b.WriteString(base)
	for _, part := range strings.FieldsFunc(args, func(r rune) bool { return r == ',' || r == ']' || r == '[' }) {
		if i := strings.LastIndex(part, "."); i >= 0 {
			part = part[i+1:]
		}
		b.WriteString(exportedName(nonAlnum.ReplaceAllString(part, "")))
	}
	return b.String()
}

func exportedName(s string) string {
	s = nonAlnum.ReplaceAllString(s, "")
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// fillObject adds the JSON fields of struct type t to s.
func (g *schemaGen) fillObject(s *Schema, t reflect.Type, where string) {
	s.Properties = map[string]*Schema{}
	for _, f := range jsonFields(t) {
		fieldWhere := where + "." + f.goName
		fs := g.fieldSchema(f, fieldWhere)
		p, err := applyTags(fs, f.tag, g)
		if err != nil {
			fail(fieldWhere, "%v", err)
		}
		s.Properties[f.name] = fs
		s.PropertyOrder = append(s.PropertyOrder, f.name)
		// Like encoding/json output: a field without omitempty is always present,
		// unless it is a pointer or has a default.
		implied := !f.omitEmpty && f.typ.Kind() != reflect.Pointer && fs.Default == nil
		if p == presenceRequired || (p == presenceDefault && implied) {
			s.addRequired(f.name)
		}
	}
}

func (g *schemaGen) fieldSchema(f jsonField, where string) *Schema {
	s := g.schemaOf(f.typ, where)
	if f.quoted {
		s = &Schema{Type: "string", Description: s.Description}
	}
	if f.typ.Kind() == reflect.Pointer && !f.omitEmpty {
		s.Nullable = true // a nil pointer is encoded as null
	}
	return s
}

func (g *schemaGen) untyped(where, format string, args ...any) {
	g.issues = append(g.issues, Issue{
		Severity: SeverityWarning,
		Rule:     "untyped-value",
		Location: where,
		Message:  fmt.Sprintf(format, args...),
	})
}

// resolve follows a reference to its component.
func (g *schemaGen) resolve(s *Schema) *Schema {
	for s != nil && s.Ref != "" {
		s = g.components[s.Ref]
	}
	return s
}

func intSchema(format string, lo, hi float64) *Schema {
	return &Schema{Type: "integer", Format: format, Minimum: &lo, Maximum: &hi}
}

func ptr[T any](v T) *T { return &v }

func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

func providedSchema(t reflect.Type) *Schema {
	v := reflect.New(t)
	if !t.Implements(schemaProviderType) {
		return v.Interface().(SchemaProvider).JSONSchema().clone()
	}
	return v.Elem().Interface().(SchemaProvider).JSONSchema().clone()
}

func jsonType(k reflect.Kind) string {
	switch k {
	case reflect.Bool:
		return "boolean"
	case reflect.String:
		return "string"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return "integer"
	}
	return ""
}

// enumValues calls the Enum method of t if it has one:
//
//	func (HiveStatus) Enum() []HiveStatus
//
// The method may return any slice type, for example []any.
func enumValues(t reflect.Type, where string) ([]any, bool) {
	if !isScalarKind(t.Kind()) {
		return nil, false
	}
	recv := reflect.New(t)
	m := recv.MethodByName("Enum")
	if !m.IsValid() {
		return nil, false
	}
	mt := m.Type()
	if mt.NumIn() != 0 || mt.NumOut() != 1 || mt.Out(0).Kind() != reflect.Slice {
		return nil, false
	}
	list := m.Call(nil)[0]
	values := make([]any, 0, list.Len())
	for i := range list.Len() {
		v, err := normalizeJSON(list.Index(i).Interface())
		if err != nil {
			fail(where, "enum value of %s: %v", t, err)
		}
		values = append(values, v)
	}
	if len(values) == 0 {
		fail(where, "%s.Enum returned no values", t)
	}
	return values, true
}

// normalizeJSON converts v to the value encoding/json would produce when
// decoding its JSON form with UseNumber.
func normalizeJSON(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return decodeJSONValue(b)
}

func decodeJSONValue(b []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// componentsSorted returns component names in alphabetical order.
func (g *schemaGen) componentsSorted() []string {
	names := make([]string, 0, len(g.components))
	for n := range g.components {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}
