package swaggerkit

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
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

func (val *validator) fail(loc, format string, args ...any) {
	val.errs = append(val.errs, FieldError{Location: loc, Message: fmt.Sprintf(format, args...)})
}

func (val *validator) check(s *Schema, v any, loc string) {
	if s == nil {
		return
	}
	nullable := s.Nullable
	s = val.gen.resolve(s)
	if v == nil {
		if !nullable && !s.Nullable && s.Type != "" {
			val.fail(loc, "must not be null")
		}
		return
	}
	if len(s.Enum) > 0 && !enumContains(s.Enum, v) {
		val.fail(loc, "must be one of %s", enumList(s.Enum))
		return
	}
	switch s.Type {
	case "":
		// any value
	case "boolean":
		if _, ok := v.(bool); !ok {
			val.fail(loc, "must be a boolean")
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			val.fail(loc, "must be a string")
			return
		}
		val.checkString(s, str, loc)
	case "integer", "number":
		n, ok := v.(json.Number)
		if !ok {
			val.fail(loc, "must be a number")
			return
		}
		val.checkNumber(s, n, loc)
	case "array":
		items, ok := v.([]any)
		if !ok {
			val.fail(loc, "must be an array")
			return
		}
		val.checkArray(s, items, loc)
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			val.fail(loc, "must be an object")
			return
		}
		val.checkObject(s, obj, loc)
	}
}

func (val *validator) checkString(s *Schema, str, loc string) {
	n := utf8.RuneCountInString(str)
	if s.MinLength != nil && n < *s.MinLength {
		val.fail(loc, "must be at least %d characters long", *s.MinLength)
	}
	if s.MaxLength != nil && n > *s.MaxLength {
		val.fail(loc, "must be at most %d characters long", *s.MaxLength)
	}
	if s.pattern != nil && !s.pattern.MatchString(str) {
		val.fail(loc, "must match pattern %s", s.Pattern)
	}
	if s.Format != "" {
		if msg := checkFormat(s.Format, str); msg != "" {
			val.fail(loc, "%s", msg)
		}
	}
}

func (val *validator) checkNumber(s *Schema, n json.Number, loc string) {
	f, ok := new(big.Float).SetString(n.String())
	if !ok {
		val.fail(loc, "must be a number")
		return
	}
	if s.Type == "integer" {
		if !f.IsInt() {
			val.fail(loc, "must be an integer")
			return
		}
		if _, err := strconv.ParseInt(n.String(), 10, 64); err != nil {
			if _, err := strconv.ParseUint(n.String(), 10, 64); err != nil {
				val.fail(loc, "must be an integer written without a fraction or exponent, in the 64-bit range")
				return
			}
		}
		if s.Format == "int32" && (f.Cmp(big.NewFloat(math.MinInt32)) < 0 || f.Cmp(big.NewFloat(math.MaxInt32)) > 0) {
			val.fail(loc, "must fit into 32 bits")
			return
		}
	} else if f.IsInf() {
		val.fail(loc, "must be a finite number")
		return
	}
	cmp := func(p *float64) int { return f.Cmp(big.NewFloat(*p)) }
	if s.Minimum != nil && cmp(s.Minimum) < 0 {
		val.fail(loc, "must be greater than or equal to %s", fmtNum(*s.Minimum))
	}
	if s.ExclusiveMinimum != nil && cmp(s.ExclusiveMinimum) <= 0 {
		val.fail(loc, "must be greater than %s", fmtNum(*s.ExclusiveMinimum))
	}
	if s.Maximum != nil && cmp(s.Maximum) > 0 {
		val.fail(loc, "must be less than or equal to %s", fmtNum(*s.Maximum))
	}
	if s.ExclusiveMaximum != nil && cmp(s.ExclusiveMaximum) >= 0 {
		val.fail(loc, "must be less than %s", fmtNum(*s.ExclusiveMaximum))
	}
	if s.MultipleOf != nil {
		q := new(big.Float).Quo(f, big.NewFloat(*s.MultipleOf))
		if !q.IsInt() {
			val.fail(loc, "must be a multiple of %s", fmtNum(*s.MultipleOf))
		}
	}
}

func (val *validator) checkArray(s *Schema, items []any, loc string) {
	if s.MinItems != nil && len(items) < *s.MinItems {
		val.fail(loc, "must contain at least %d items", *s.MinItems)
	}
	if s.MaxItems != nil && len(items) > *s.MaxItems {
		val.fail(loc, "must contain at most %d items", *s.MaxItems)
		return // do not spend time on items of an oversized array
	}
	if s.UniqueItems {
		seen := make(map[string]bool, len(items))
		for _, it := range items {
			key := canonical(it)
			if seen[key] {
				val.fail(loc, "must not contain duplicate items")
				break
			}
			seen[key] = true
		}
	}
	for i, it := range items {
		val.check(s.Items, it, loc+"["+strconv.Itoa(i)+"]")
	}
}

func (val *validator) checkObject(s *Schema, obj map[string]any, loc string) {
	if s.MinProperties != nil && len(obj) < *s.MinProperties {
		val.fail(loc, "must contain at least %d entries", *s.MinProperties)
	}
	if s.MaxProperties != nil && len(obj) > *s.MaxProperties {
		val.fail(loc, "must contain at most %d entries", *s.MaxProperties)
		return
	}
	for _, name := range s.Required {
		if _, ok := obj[name]; !ok {
			val.fail(join(loc, name), "is required")
		}
	}
	for _, name := range s.propertyNames() {
		prop := s.Properties[name]
		v, ok := obj[name]
		if !ok {
			if val.fillDefaults && prop.Default != nil {
				obj[name] = prop.Default
				val.changed = true
			}
			continue
		}
		val.check(prop, v, join(loc, name))
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, known := s.Properties[k]; known {
			continue
		}
		switch {
		case s.AdditionalProperties != nil:
			val.check(s.AdditionalProperties, obj[k], join(loc, k))
		case s.closed && val.strict:
			val.fail(join(loc, k), "unknown field")
		}
	}
}

func join(loc, name string) string {
	if loc == "" {
		return name
	}
	return loc + "." + name
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
