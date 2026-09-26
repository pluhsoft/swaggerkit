package swaggerkit

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// Parameter locations and their struct tags.
const (
	inPath   = "path"
	inQuery  = "query"
	inHeader = "header"
	inCookie = "cookie"
)

var paramLocations = []string{inPath, inQuery, inHeader, inCookie}

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

type inputPlan struct {
	typ    reflect.Type
	params []*paramPlan
	body   *bodyPlan
}

type paramPlan struct {
	in       string
	name     string
	index    []int
	schema   *Schema
	required bool
}

type bodyPlan struct {
	index       []int // nil: the whole input is the body
	typ         reflect.Type
	schema      *Schema
	required    bool
	description string // doc tag of the Body field
}

func (p *inputPlan) empty() bool { return len(p.params) == 0 && p.body == nil }

// analyzeInput reads the input type of a handler.
func analyzeInput(g *schemaGen, t reflect.Type, method string, pathParams []string, where string) *inputPlan {
	plan := &inputPlan{typ: t}
	if t.Kind() == reflect.Struct {
		var untagged []string
		collectInput(g, t, nil, plan, &untagged)
		if plan.empty() && len(untagged) > 0 {
			plan.body = &bodyPlan{typ: t, required: true} // the whole struct is the body
		} else if len(untagged) > 0 {
			fail(t.Name(), "field %s needs a path, query, header or cookie tag, or must be moved into Body", untagged[0])
		}
	} else {
		plan.body = &bodyPlan{typ: t, required: true}
	}

	if b := plan.body; b != nil {
		if method == http.MethodGet || method == http.MethodHead {
			fail("input", "%s requests cannot have a body; tag the fields of %s as query parameters", method, t)
		}
		b.schema = g.schemaOf(b.typ, "body")
		if b.index != nil && b.typ.Kind() == reflect.Pointer {
			b.required = false
		}
	}

	declared := map[string]bool{}
	for _, p := range plan.params {
		if p.in == inPath {
			if !slices.Contains(pathParams, p.name) {
				fail("input", "field for path parameter %q, but the path has no {%s}", p.name, p.name)
			}
			declared[p.name] = true
		}
	}
	for _, name := range pathParams {
		if !declared[name] {
			fail("input", "path parameter {%s} has no field tagged path:%q in %s", name, name, t)
		}
	}
	return plan
}

func collectInput(g *schemaGen, t reflect.Type, index []int, plan *inputPlan, untagged *[]string) {
	for i := range t.NumField() {
		sf := t.Field(i)
		idx := append(slices.Clone(index), i)
		loc, name := paramTag(sf.Tag)
		switch {
		case loc != "":
			if !sf.IsExported() {
				fail(t.Name()+"."+sf.Name, "parameter fields must be exported")
			}
			plan.params = append(plan.params, newParamPlan(g, sf, idx, loc, name, t.Name()+"."+sf.Name))
		case sf.Name == "Body" && !sf.Anonymous:
			if plan.body != nil {
				fail(t.Name(), "more than one Body field")
			}
			plan.body = &bodyPlan{index: idx, typ: sf.Type, required: true, description: sf.Tag.Get(tagDoc)}
		case sf.Anonymous && deref(sf.Type).Kind() == reflect.Struct:
			if sf.Type.Kind() == reflect.Pointer {
				fail(t.Name()+"."+sf.Name, "embed parameter structs by value, not by pointer")
			}
			collectInput(g, sf.Type, idx, plan, untagged)
		case sf.IsExported():
			*untagged = append(*untagged, sf.Name)
		}
	}
}

func paramTag(tag reflect.StructTag) (loc, name string) {
	for _, l := range paramLocations {
		if n, ok := tag.Lookup(l); ok {
			return l, n
		}
	}
	return "", ""
}

func newParamPlan(g *schemaGen, sf reflect.StructField, index []int, loc, name, where string) *paramPlan {
	if name == "" {
		fail(where, "%s tag needs a name, e.g. %s:\"id\"", loc, loc)
	}
	elem := deref(sf.Type)
	if elem.Kind() == reflect.Slice && !isTextType(elem) {
		if loc == inPath || loc == inCookie {
			fail(where, "%s parameters cannot be slices", loc)
		}
		elem = deref(elem.Elem())
	}
	if !isTextType(elem) {
		fail(where, "parameter type %s is not supported; use strings, numbers, booleans, time.Time or encoding.TextUnmarshaler types", sf.Type)
	}
	s := g.schemaOf(sf.Type, where)
	p, err := applyTags(s, sf.Tag, g)
	if err != nil {
		fail(where, "%v", err)
	}
	required := p == presenceRequired || loc == inPath
	if loc == inPath && p == presenceOptional {
		fail(where, "path parameters are always required")
	}
	if loc == inHeader {
		name = http.CanonicalHeaderKey(name)
	}
	return &paramPlan{in: loc, name: name, index: index, schema: s, required: required}
}

// isTextType reports whether a parameter value can be parsed from a single string.
func isTextType(t reflect.Type) bool {
	t = deref(t)
	return t == timeType || isScalarKind(t.Kind()) || reflect.PointerTo(t).Implements(textUnmarshalerType)
}

// bind decodes and validates the request into a new In value.
func (p *inputPlan) bind(a *API, w http.ResponseWriter, r *http.Request) (reflect.Value, *Error) {
	in := reflect.New(p.typ).Elem()
	var errs []FieldError
	var query map[string][]string
	for _, pp := range p.params {
		var raw []string
		switch pp.in {
		case inPath:
			raw = []string{r.PathValue(pp.name)}
		case inQuery:
			if query == nil {
				query = r.URL.Query()
			}
			raw = query[pp.name]
		case inHeader:
			raw = r.Header.Values(pp.name)
		case inCookie:
			if c, err := r.Cookie(pp.name); err == nil {
				raw = []string{c.Value}
			}
		}
		errs = append(errs, pp.set(a, in.FieldByIndex(pp.index), raw)...)
	}
	if p.body != nil {
		target := in
		if p.body.index != nil {
			target = in.FieldByIndex(p.body.index)
		}
		if e := p.body.decode(a, w, r, target); e != nil {
			if e.Status != http.StatusUnprocessableEntity {
				return in, e
			}
			errs = append(errs, e.Errors...)
		}
	}
	if len(errs) > 0 {
		return in, validationError(http.StatusUnprocessableEntity, "request validation failed", errs)
	}
	return in, nil
}

var jsonNumberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// set converts raw parameter values, validates them against the schema and
// stores them in field.
func (pp *paramPlan) set(a *API, field reflect.Value, raw []string) []FieldError {
	loc := pp.in + "." + pp.name
	a.mu.RLock()
	s := a.gen.resolve(pp.schema)
	a.mu.RUnlock()
	isArray := s.Type == "array"
	if !isArray && len(raw) > 1 {
		return []FieldError{{loc, "must be given once"}}
	}
	if len(raw) == 1 && raw[0] == "" && s.Type != "string" {
		raw = nil // "?limit=" means the value is missing
	}

	var value any
	switch {
	case len(raw) == 0 && pp.schema.Default != nil:
		value = pp.schema.Default
	case len(raw) == 0 && pp.required:
		return []FieldError{{loc, "is required"}}
	case len(raw) == 0:
		return nil
	case isArray:
		items := a.gen.resolve(s.Items)
		list := make([]any, len(raw))
		for i, v := range raw {
			item, ok := paramValue(items, v)
			if !ok {
				return []FieldError{{fmt.Sprintf("%s[%d]", loc, i), "must be " + typeWord(items)}}
			}
			list[i] = item
		}
		value = list
	default:
		v, ok := paramValue(s, raw[0])
		if !ok {
			return []FieldError{{loc, "must be " + typeWord(s)}}
		}
		value = v
	}

	a.mu.RLock()
	errs := validateValue(a.gen, pp.schema, value, loc)
	a.mu.RUnlock()
	if len(errs) > 0 {
		return errs
	}
	data, err := marshalJSON(value)
	if err == nil {
		err = json.Unmarshal(data, field.Addr().Interface())
	}
	if err != nil {
		return []FieldError{{loc, "has an invalid value"}}
	}
	return nil
}

// paramValue converts a raw string to a JSON value of the schema type.
func paramValue(s *Schema, raw string) (any, bool) {
	switch s.Type {
	case "integer", "number":
		if !jsonNumberPattern.MatchString(raw) {
			return nil, false
		}
		return json.Number(raw), true
	case "boolean":
		switch raw {
		case "true", "1":
			return true, true
		case "false", "0":
			return false, true
		}
		return nil, false
	}
	return raw, true
}

func typeWord(s *Schema) string {
	switch s.Type {
	case "integer":
		return "an integer"
	case "number":
		return "a number"
	case "boolean":
		return "true or false"
	}
	return "a string"
}

// decode reads, validates and decodes the JSON body into target.
func (b *bodyPlan) decode(a *API, w http.ResponseWriter, r *http.Request, target reflect.Value) *Error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, a.maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf("request body must not exceed %d bytes", a.maxBodyBytes))
		}
		return BadRequest("could not read the request body")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		if b.required {
			return validationError(http.StatusUnprocessableEntity, "", []FieldError{{"body", "is required"}})
		}
		return nil
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return NewError(http.StatusUnsupportedMediaType, "Content-Type must be application/json")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return BadRequest("request body is not valid JSON: " + jsonErrorText(err))
	}
	if _, err := dec.Token(); err != io.EOF {
		return BadRequest("request body must contain a single JSON value")
	}

	a.mu.RLock()
	val := validator{gen: a.gen, strict: a.strict, fillDefaults: true}
	val.check(b.schema, value, "body")
	a.mu.RUnlock()
	if len(val.errs) > 0 {
		return validationError(http.StatusUnprocessableEntity, "", val.errs)
	}
	if val.changed {
		if data, err = marshalJSON(value); err != nil {
			return &Error{Status: http.StatusInternalServerError, Err: err}
		}
	}
	if err := json.Unmarshal(data, target.Addr().Interface()); err != nil {
		loc := "body"
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			loc += "." + typeErr.Field
		}
		return validationError(http.StatusUnprocessableEntity, "", []FieldError{{loc, "has an invalid value"}})
	}
	return nil
}

func isJSONContentType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "application/json" || (strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json"))
}

// jsonErrorText returns a decoding error message without Go type names.
func jsonErrorText(err error) string {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Sprintf("%s (at byte %d)", syntax.Error(), syntax.Offset)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected end of input"
	}
	return "malformed input"
}
