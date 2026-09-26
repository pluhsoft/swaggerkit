package swaggerkit

import (
	"reflect"
	"slices"
	"strings"
)

// jsonField is a struct field as encoding/json sees it.
type jsonField struct {
	name      string // JSON name
	goName    string
	index     []int
	typ       reflect.Type
	tag       reflect.StructTag
	omitEmpty bool // omitempty or omitzero
	quoted    bool // ",string" option
	tagged    bool // name comes from the json tag
}

// jsonFields returns the fields encoding/json encodes for struct type t,
// including fields promoted from embedded structs, in declaration order.
func jsonFields(t reflect.Type) []jsonField {
	type queued struct {
		typ   reflect.Type
		index []int
	}
	var fields []jsonField
	next := []queued{{t, nil}}
	visited := map[reflect.Type]bool{}
	seen := map[string]bool{} // names used at a shallower depth hide deeper fields
	for len(next) > 0 {
		current := next
		next = nil
		var level []jsonField
		for _, q := range current {
			if visited[q.typ] {
				continue
			}
			visited[q.typ] = true
			for i := range q.typ.NumField() {
				sf := q.typ.Field(i)
				ft := sf.Type
				if sf.Anonymous {
					if ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					if !sf.IsExported() && ft.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, opts, _ := strings.Cut(tag, ",")
				index := append(slices.Clone(q.index), i)
				if name == "" && sf.Anonymous && ft.Kind() == reflect.Struct {
					next = append(next, queued{ft, index})
					continue
				}
				f := jsonField{
					name:   name,
					goName: sf.Name,
					index:  index,
					typ:    sf.Type,
					tag:    sf.Tag,
					tagged: name != "",
				}
				if f.name == "" {
					f.name = sf.Name
				}
				for _, o := range strings.Split(opts, ",") {
					switch o {
					case "omitempty", "omitzero":
						f.omitEmpty = true
					case "string":
						f.quoted = isScalarKind(deref(sf.Type).Kind())
					}
				}
				level = append(level, f)
			}
		}
		var fresh []jsonField
		for _, f := range level {
			if !seen[f.name] {
				fresh = append(fresh, f)
			}
		}
		for _, f := range fresh {
			seen[f.name] = true
		}
		fields = append(fields, dominantFields(fresh)...)
	}
	slices.SortFunc(fields, func(a, b jsonField) int { return slices.Compare(a.index, b.index) })
	return fields
}

// dominantFields resolves name conflicts within one embedding depth:
// a single tagged field wins, otherwise all conflicting fields are dropped.
func dominantFields(level []jsonField) []jsonField {
	byName := map[string][]jsonField{}
	var order []string
	for _, f := range level {
		if _, ok := byName[f.name]; !ok {
			order = append(order, f.name)
		}
		byName[f.name] = append(byName[f.name], f)
	}
	var out []jsonField
	for _, name := range order {
		group := byName[name]
		if len(group) == 1 {
			out = append(out, group[0])
			continue
		}
		var tagged []jsonField
		for _, f := range group {
			if f.tagged {
				tagged = append(tagged, f)
			}
		}
		if len(tagged) == 1 {
			out = append(out, tagged[0])
		}
	}
	return out
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func isScalarKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}
