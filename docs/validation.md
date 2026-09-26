---
title: Validation
nav_order: 3
---

# Validation

Rules are written once in struct tags. They validate requests and appear in the OpenAPI document.

```go
type NewHive struct {
	Name   string     `json:"name" doc:"Name painted on the hive" validate:"required,min=1,max=64" example:"Linden"`
	Status HiveStatus `json:"status,omitempty" default:"empty"`
	Tags   []string   `json:"tags,omitempty" validate:"max=10,unique,dive,min=1,max=32"`
	Code   string     `json:"code,omitempty" pattern:"^[A-Z]{3}$"`
}
```

## Tags

| Tag          | Meaning                                                 |
| ------------ | ------------------------------------------------------- |
| `json`       | name, `omitempty`/`omitzero`, `-`, `string`, as in `encoding/json` |
| `validate`   | rules, comma-separated                                  |
| `pattern`    | regular expression for strings (separate tag: it may contain commas) |
| `default`    | value used when the field or parameter is missing       |
| `doc`        | description                                             |
| `example`    | example value; JSON for arrays and objects              |
| `deprecated` | `"true"` marks the field deprecated                     |

## Required fields

Like the JSON that Go produces:

- a field without `omitempty`/`omitzero` is required;
- pointers, fields with `omitempty` and fields with `default` are optional;
- `validate:"required"` or `validate:"optional"` overrides it.

`required` means the field is present. `0`, `false` and `""` are valid values.
Query, header and cookie parameters are optional unless marked `required`; path parameters are always required.

## Rules

| Rule            | Numbers        | Strings (characters) | Slices          | Maps              |
| --------------- | -------------- | -------------------- | --------------- | ----------------- |
| `min=N`         | ≥ N            | length ≥ N           | items ≥ N       | entries ≥ N       |
| `max=N`         | ≤ N            | length ≤ N           | items ≤ N       | entries ≤ N       |
| `len=N`         |                | length = N           | items = N       | entries = N       |
| `gt`, `gte`, `lt`, `lte` | > ≥ < ≤ |                     |                 |                   |
| `multipleOf=N`  | multiple of N  |                      |                 |                   |
| `oneof=a b c`   | one of         | one of               |                 |                   |
| `unique`        |                |                      | no duplicates   |                   |
| `dive`          |                |                      | following rules apply to items | … to values |

String formats: `email`, `url` (`uri`), `uuid`, `ipv4`, `ipv6`, `hostname`, `date`, `datetime`.

Type limits are implied: `uint8` is 0–255, `int32` must fit into 32 bits, integers reject fractions.

Unknown rules and rules that do not fit the type panic at registration: typos cannot silently disable validation.

## Unknown fields

JSON fields that the Go type does not declare are rejected (`unknown field`). Allow them with `swaggerkit.WithUnknownFields()`. Field names are case-sensitive.

## Enums

Give the type an `Enum` method. Values are validated and documented as a shared schema.

```go
type HiveStatus string

const (
	StatusActive HiveStatus = "active"
	StatusEmpty  HiveStatus = "empty"
)

func (HiveStatus) Enum() []HiveStatus { return []HiveStatus{StatusActive, StatusEmpty} }
```

For a one-off list use `validate:"oneof=active empty"`.

## Type descriptions

A `Doc` method describes the type as a whole (field descriptions come from `doc` tags):

```go
func (Hive) Doc() string { return "A beehive in the apiary." }
```

## Custom types

Types with `UnmarshalText`/`MarshalText` are strings. For anything else implement `swaggerkit.SchemaProvider`:

```go
func (Point) JSONSchema() *swaggerkit.Schema {
	return &swaggerkit.Schema{Type: "string", Pattern: `^\d+,\d+$`, Description: "x,y"}
}
```

## Limits

Request bodies are limited to 1 MiB (`WithMaxBodyBytes`). Use `max` on strings, slices and maps in request types; [`Lint`](openapi#lint) lists the unbounded ones. Set timeouts on `http.Server`.
