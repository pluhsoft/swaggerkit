package swaggerkit

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FieldError describes one invalid value in a request.
type FieldError struct {
	// Location of the value: "body.frames[2].honeyKg", "query.limit", "path.hiveId".
	Location string `json:"location"`
	Message  string `json:"message"`
}

// validator checks decoded JSON values (nil, bool, json.Number, string,
// []any, map[string]any) against a schema.
type validator struct {
	gen          *schemaGen
	strict       bool // reject properties that Go structs do not declare
	fillDefaults bool // set missing object properties that have a default
	changed      bool // a default was filled in
	errs         []FieldError
}

func validateValue(g *schemaGen, s *Schema, v any, loc string) []FieldError {
	val := validator{gen: g}
	val.check(s, v, loc)
	return val.errs
}

// check validates v; loc names the root value, e.g. "body". Errors are
// sorted by location, so the output does not depend on map order.
func (val *validator) check(s *Schema, v any, loc string) {
	val.checkAt(s, v, &valuePath{key: loc, index: -1})
	slices.SortStableFunc(val.errs, func(a, b FieldError) int { return strings.Compare(a.Location, b.Location) })
}

// valuePath is the location of a value. It is turned into a string only when
// validation fails, so valid requests do not pay for it.
type valuePath struct {
	parent *valuePath
	key    string
	index  int // array index, or -1 for an object key
}

func (p *valuePath) String() string {
	if p.parent == nil {
		return p.key
	}
	prefix := p.parent.String()
	switch {
	case p.index >= 0:
		return prefix + "[" + strconv.Itoa(p.index) + "]"
	case prefix == "":
		return p.key
	}
	return prefix + "." + p.key
}

func (val *validator) fail(p *valuePath, format string, args ...any) {
	val.errs = append(val.errs, FieldError{Location: p.String(), Message: fmt.Sprintf(format, args...)})
}

func (val *validator) checkAt(s *Schema, v any, p *valuePath) {
	if s == nil {
		return
	}
	nullable := s.Nullable
	s = val.gen.resolve(s)
	if v == nil {
		if !nullable && !s.Nullable && s.Type != "" {
			val.fail(p, "must not be null")
		}
		return
	}
	if len(s.Enum) > 0 && !enumContains(s.Enum, v) {
		val.fail(p, "must be one of %s", enumList(s.Enum))
		return
	}
	switch s.Type {
	case "":
		// any value
	case "boolean":
		if _, ok := v.(bool); !ok {
			val.fail(p, "must be a boolean")
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			val.fail(p, "must be a string")
			return
		}
		val.checkString(s, str, p)
	case "integer", "number":
		n, ok := v.(json.Number)
		if !ok {
			val.fail(p, "must be a number")
			return
		}
		val.checkNumber(s, n, p)
	case "array":
		items, ok := v.([]any)
		if !ok {
			val.fail(p, "must be an array")
			return
		}
		val.checkArray(s, items, p)
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			val.fail(p, "must be an object")
			return
		}
		val.checkObject(s, obj, p)
	}
}

func (val *validator) checkString(s *Schema, str string, p *valuePath) {
	if s.MinLength != nil || s.MaxLength != nil {
		n := utf8.RuneCountInString(str)
		if s.MinLength != nil && n < *s.MinLength {
			val.fail(p, "must be at least %d characters long", *s.MinLength)
		}
		if s.MaxLength != nil && n > *s.MaxLength {
			val.fail(p, "must be at most %d characters long", *s.MaxLength)
		}
	}
	if s.pattern != nil && !s.pattern.MatchString(str) {
		val.fail(p, "must match pattern %s", s.Pattern)
	}
	if s.Format != "" {
		if msg := checkFormat(s.Format, str); msg != "" {
			val.fail(p, "%s", msg)
		}
	}
}

func (val *validator) checkNumber(s *Schema, n json.Number, p *valuePath) {
	str := n.String()
	var f float64
	if s.Type == "integer" {
		i, err := strconv.ParseInt(str, 10, 64)
		switch {
		case err == nil:
			if s.Format == "int32" && (i < math.MinInt32 || i > math.MaxInt32) {
				val.fail(p, "must fit into 32 bits")
				return
			}
			f = float64(i)
		default:
			u, uerr := strconv.ParseUint(str, 10, 64)
			if uerr == nil {
				f = float64(u)
				break
			}
			if fv, ferr := strconv.ParseFloat(str, 64); ferr == nil && fv == math.Trunc(fv) {
				val.fail(p, "must be an integer written without a fraction or exponent, in the 64-bit range")
			} else {
				val.fail(p, "must be an integer")
			}
			return
		}
	} else {
		fv, err := strconv.ParseFloat(str, 64)
		if err != nil {
			val.fail(p, "must be a finite number")
			return
		}
		f = fv
	}
	if s.Minimum != nil && f < *s.Minimum {
		val.fail(p, "must be greater than or equal to %s", fmtNum(*s.Minimum))
	}
	if s.ExclusiveMinimum != nil && f <= *s.ExclusiveMinimum {
		val.fail(p, "must be greater than %s", fmtNum(*s.ExclusiveMinimum))
	}
	if s.Maximum != nil && f > *s.Maximum {
		val.fail(p, "must be less than or equal to %s", fmtNum(*s.Maximum))
	}
	if s.ExclusiveMaximum != nil && f >= *s.ExclusiveMaximum {
		val.fail(p, "must be less than %s", fmtNum(*s.ExclusiveMaximum))
	}
	if s.MultipleOf != nil {
		// Exact decimal arithmetic: 0.3 is a multiple of 0.1.
		exact, _ := new(big.Float).SetString(str)
		if q := new(big.Float).Quo(exact, big.NewFloat(*s.MultipleOf)); !q.IsInt() {
			val.fail(p, "must be a multiple of %s", fmtNum(*s.MultipleOf))
		}
	}
}

func (val *validator) checkArray(s *Schema, items []any, p *valuePath) {
	if s.MinItems != nil && len(items) < *s.MinItems {
		val.fail(p, "must contain at least %d items", *s.MinItems)
	}
	if s.MaxItems != nil && len(items) > *s.MaxItems {
		val.fail(p, "must contain at most %d items", *s.MaxItems)
		return // do not spend time on items of an oversized array
	}
	if s.UniqueItems {
		seen := make(map[string]bool, len(items))
		for _, it := range items {
			key := canonical(it)
			if seen[key] {
				val.fail(p, "must not contain duplicate items")
				break
			}
			seen[key] = true
		}
	}
	item := valuePath{parent: p}
	for i, it := range items {
		item.index = i
		val.checkAt(s.Items, it, &item)
	}
}

func (val *validator) checkObject(s *Schema, obj map[string]any, p *valuePath) {
	if s.MinProperties != nil && len(obj) < *s.MinProperties {
		val.fail(p, "must contain at least %d entries", *s.MinProperties)
	}
	if s.MaxProperties != nil && len(obj) > *s.MaxProperties {
		val.fail(p, "must contain at most %d entries", *s.MaxProperties)
		return
	}
	field := valuePath{parent: p, index: -1}
	for _, name := range s.Required {
		if _, ok := obj[name]; !ok {
			field.key = name
			val.fail(&field, "is required")
		}
	}
	for name, prop := range s.Properties {
		v, ok := obj[name]
		if !ok {
			if val.fillDefaults && prop.Default != nil {
				obj[name] = prop.Default
				val.changed = true
			}
			continue
		}
		field.key = name
		val.checkAt(prop, v, &field)
	}
	if len(obj) <= len(s.Properties) && s.AdditionalProperties == nil && !(s.closed && val.strict) {
		return
	}
	for k, v := range obj {
		if _, known := s.Properties[k]; known {
			continue
		}
		field.key = k
		switch {
		case s.AdditionalProperties != nil:
			val.checkAt(s.AdditionalProperties, v, &field)
		case s.closed && val.strict:
			val.fail(&field, "unknown field")
		}
	}
}

func enumContains(enum []any, v any) bool {
	key := canonical(v)
	for _, e := range enum {
		if canonical(e) == key {
			return true
		}
	}
	return false
}

func enumList(enum []any) string {
	b, _ := marshalJSON(enum)
	return string(b)
}

// canonical returns a comparable form of a JSON value. Numbers are compared by value.
func canonical(v any) string {
	if n, ok := v.(json.Number); ok {
		if f, ok := new(big.Float).SetString(n.String()); ok {
			return "n:" + f.Text('g', -1)
		}
	}
	if s, ok := v.(string); ok {
		return "s:" + s
	}
	b, _ := marshalJSON(v)
	return "j:" + string(b)
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
