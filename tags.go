package swaggerkit

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Struct tags read by swaggerkit, in addition to json:
//
//	doc:"Honey in kilograms"      description
//	example:"12.5"                example value
//	default:"10"                  default value, applied when the value is missing
//	deprecated:"true"             marks the field as deprecated
//	pattern:"^[a-z]+$"            regular expression for strings
//	validate:"required,min=1"     validation rules, see validation.md
const (
	tagDoc        = "doc"
	tagExample    = "example"
	tagDefault    = "default"
	tagDeprecated = "deprecated"
	tagPattern    = "pattern"
	tagValidate   = "validate"
)

// presence says whether a validate tag makes a field required or optional.
type presence int

const (
	presenceDefault presence = iota
	presenceRequired
	presenceOptional
)

// applyTags applies documentation and validation tags to s.
func applyTags(s *Schema, tag reflect.StructTag, g *schemaGen) (p presence, err error) {
	if doc := tag.Get(tagDoc); doc != "" {
		s.Description = doc
	}
	if tag.Get(tagDeprecated) == "true" {
		s.Deprecated = true
	}
	if p, ok := tag.Lookup(tagPattern); ok {
		target := g.resolve(s)
		if target.Type != "string" || s.Ref != "" {
			return presenceDefault, fmt.Errorf("pattern applies to string fields only")
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return presenceDefault, fmt.Errorf("invalid pattern: %w", err)
		}
		s.Pattern, s.pattern = p, re
	}
	if rules, ok := tag.Lookup(tagValidate); ok {
		if p, err = applyRules(s, rules, g); err != nil {
			return presenceDefault, err
		}
	}
	// Examples and defaults are parsed last so enum rules can check them.
	if ex, ok := tag.Lookup(tagExample); ok {
		v, err := parseTagValue(g.resolve(s), ex)
		if err != nil {
			return presenceDefault, fmt.Errorf("example: %w", err)
		}
		s.Examples = []any{v}
	}
	if def, ok := tag.Lookup(tagDefault); ok {
		v, err := parseTagValue(g.resolve(s), def)
		if err != nil {
			return presenceDefault, fmt.Errorf("default: %w", err)
		}
		if errs := validateValue(g, s, v, "default"); len(errs) > 0 {
			return presenceDefault, fmt.Errorf("default %q is invalid: %s", def, errs[0].Message)
		}
		s.Default = v
	}
	return p, nil
}

// applyRules applies a validate tag. Rules after "dive" apply to the elements
// of a slice or the values of a map.
func applyRules(s *Schema, rules string, g *schemaGen) (p presence, err error) {
	target := s
	for _, rule := range strings.Split(rules, ",") {
		rule = strings.TrimSpace(rule)
		name, arg, hasArg := strings.Cut(rule, "=")
		if rule == "" {
			continue
		}
		if name == "required" || name == "optional" {
			if target != s {
				return presenceDefault, fmt.Errorf("%s cannot follow dive", name)
			}
			if p != presenceDefault {
				return presenceDefault, fmt.Errorf("use either required or optional")
			}
			p = presenceRequired
			if name == "optional" {
				p = presenceOptional
			}
			continue
		}
		resolved := g.resolve(target)
		if name == "dive" {
			switch {
			case resolved.Type == "array" && target.Ref == "":
				target = resolved.Items
			case resolved.AdditionalProperties != nil && target.Ref == "":
				target = resolved.AdditionalProperties
			default:
				return presenceDefault, fmt.Errorf("dive applies to slices and maps only")
			}
			continue
		}
		if target.Ref != "" {
			return presenceDefault, fmt.Errorf("rule %q cannot be applied to %s; put rules on its fields", name, target.Ref)
		}
		if _, isFormat := formatRules[name]; !isFormat && !slices.Contains(knownRules, name) {
			return presenceDefault, fmt.Errorf("unknown rule %q", name)
		}
		if needsArg(name) != hasArg {
			if hasArg {
				return presenceDefault, fmt.Errorf("rule %q takes no argument", name)
			}
			return presenceDefault, fmt.Errorf("rule %q needs an argument, e.g. %s=1", name, name)
		}
		if err := applyRule(target, name, arg); err != nil {
			return presenceDefault, fmt.Errorf("rule %q: %w", rule, err)
		}
	}
	return p, nil
}

var knownRules = []string{"min", "max", "len", "gt", "gte", "lt", "lte", "oneof", "multipleOf", "unique"}

func needsArg(rule string) bool {
	switch rule {
	case "min", "max", "len", "gt", "gte", "lt", "lte", "oneof", "multipleOf":
		return true
	}
	return false
}

var formatRules = map[string]string{
	"email":    "email",
	"url":      "uri",
	"uri":      "uri",
	"uuid":     "uuid",
	"ipv4":     "ipv4",
	"ipv6":     "ipv6",
	"hostname": "hostname",
	"date":     "date",
	"datetime": "date-time",
}

func applyRule(s *Schema, name, arg string) error {
	if format, ok := formatRules[name]; ok {
		if s.Type != "string" {
			return fmt.Errorf("applies to strings only")
		}
		s.Format = format
		return nil
	}
	switch name {
	case "unique":
		if s.Type != "array" {
			return fmt.Errorf("applies to slices only")
		}
		s.UniqueItems = true
		return nil
	case "oneof":
		var values []any
		for _, raw := range strings.Fields(arg) {
			v, err := parseTagValue(s, raw)
			if err != nil {
				return err
			}
			values = append(values, v)
		}
		s.Enum = values
		return nil
	}

	n, err := strconv.ParseFloat(arg, 64)
	if err != nil {
		return fmt.Errorf("argument must be a number")
	}
	numeric := s.Type == "integer" || s.Type == "number"
	switch name {
	case "gt", "gte", "lt", "lte", "multipleOf":
		if !numeric {
			return fmt.Errorf("applies to numbers only; use min, max or len for lengths")
		}
		switch name {
		case "gt":
			s.ExclusiveMinimum = &n
		case "gte":
			s.Minimum = &n
		case "lt":
			s.ExclusiveMaximum = &n
		case "lte":
			s.Maximum = &n
		case "multipleOf":
			if n <= 0 {
				return fmt.Errorf("argument must be positive")
			}
			s.MultipleOf = &n
		}
		return nil
	}

	// min, max, len
	if numeric {
		if name == "len" {
			return fmt.Errorf("applies to strings, slices and maps only")
		}
		if name == "min" {
			s.Minimum = &n
		} else {
			s.Maximum = &n
		}
		return nil
	}
	if n < 0 || n != float64(int(n)) {
		return fmt.Errorf("length must be a non-negative integer")
	}
	size := int(n)
	var lo, hi **int
	switch {
	case s.Type == "string":
		lo, hi = &s.MinLength, &s.MaxLength
	case s.Type == "array":
		lo, hi = &s.MinItems, &s.MaxItems
	case s.Type == "object" && s.AdditionalProperties != nil:
		lo, hi = &s.MinProperties, &s.MaxProperties
	default:
		return fmt.Errorf("applies to numbers, strings, slices and maps only")
	}
	switch name {
	case "min":
		*lo = &size
	case "max":
		*hi = &size
	case "len":
		*lo, *hi = &size, ptr(size)
	}
	return nil
}

// parseTagValue parses a tag value (example, default, oneof) for schema s.
func parseTagValue(s *Schema, raw string) (any, error) {
	if s == nil {
		return raw, nil
	}
	switch s.Type {
	case "string":
		return raw, nil
	case "integer":
		if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
			return nil, fmt.Errorf("%q is not an integer", raw)
		}
		return json.Number(raw), nil
	case "number":
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		return json.Number(raw), nil
	case "boolean":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is not a boolean", raw)
		}
		return b, nil
	}
	v, err := decodeJSONValue([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("%q is not valid JSON", raw)
	}
	return v, nil
}
