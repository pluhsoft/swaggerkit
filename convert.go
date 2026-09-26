package swaggerkit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Format is a document format for [API.Document].
type Format string

// Document formats. OpenAPI 3.1 is the source; the others are converted from it.
const (
	FormatOpenAPI31 Format = "openapi-3.1"
	FormatOpenAPI30 Format = "openapi-3.0" // tools without 3.1 support
	FormatSwagger20 Format = "swagger-2.0" // legacy tools and API gateways
)

// Document returns the API description in the given format as indented JSON.
//
// Converting to older formats loses what they cannot express: Swagger 2.0 has
// no cookie parameters, no OpenID Connect and one schema per response.
func (a *API) Document(f Format) ([]byte, error) {
	if f == FormatOpenAPI31 || f == "" {
		return a.OpenAPI()
	}
	a.mu.RLock()
	cached := a.specs[f]
	a.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	source, err := a.OpenAPI()
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(source))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var out []byte
	switch f {
	case FormatOpenAPI30:
		out, err = marshalDocument(toOpenAPI30(doc), "openapi")
	case FormatSwagger20:
		out, err = marshalDocument(toSwagger20(doc), "swagger")
	default:
		return nil, &Error{Status: http.StatusInternalServerError, Err: errUnknownFormat(f)}
	}
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.specs[f] = out
	a.mu.Unlock()
	return out, nil
}

type errUnknownFormat Format

func (e errUnknownFormat) Error() string { return "swaggerkit: unknown document format " + string(e) }

// DocumentHandler serves the document in the given format.
func (a *API) DocumentHandler(f Format) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc, err := a.Document(f)
		if err != nil {
			a.writeError(r.Context(), w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(doc)
	})
}

// marshalDocument writes the version key first, then the rest sorted, indented.
func marshalDocument(doc map[string]any, versionKey string) ([]byte, error) {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		if k != versionKey {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range append([]string{versionKey}, keys...) {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := marshalJSON(k)
		vb, err := marshalJSON(doc[k])
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// JSON tree helpers.

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

func arr(v any) []any { a, _ := v.([]any); return a }

func str(v any) string { s, _ := v.(string); return s }

// eachSchema calls f on every schema inside s, depth first, then on s.
func eachSchema(s map[string]any, f func(map[string]any)) {
	if s == nil {
		return
	}
	for _, p := range obj(s["properties"]) {
		eachSchema(obj(p), f)
	}
	for _, key := range []string{"items", "additionalProperties"} {
		eachSchema(obj(s[key]), f)
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		for _, sub := range arr(s[key]) {
			eachSchema(obj(sub), f)
		}
	}
	f(s)
}

// downgradeSchema turns an OpenAPI 3.1 schema into the OpenAPI 3.0 dialect.
// nullKey is "nullable" for OpenAPI 3.0 and "x-nullable" for Swagger 2.0.
func downgradeSchema(s map[string]any, nullKey string) {
	if types := arr(s["type"]); types != nil {
		for _, t := range types {
			if t == "null" {
				s[nullKey] = true
			} else {
				s["type"] = t
			}
		}
	}
	// {"anyOf": [{"$ref": X}, {"type": "null"}]} → {"allOf": [{"$ref": X}], nullable}
	if alts := arr(s["anyOf"]); len(alts) == 2 && obj(alts[1])["type"] == "null" {
		delete(s, "anyOf")
		s["allOf"] = []any{alts[0]}
		s[nullKey] = true
	}
	// Siblings of $ref are ignored before 3.1: wrap the reference.
	if ref, ok := s["$ref"]; ok && len(s) > 1 {
		delete(s, "$ref")
		s["allOf"] = []any{map[string]any{"$ref": ref}}
	}
	if ex := arr(s["examples"]); ex != nil {
		delete(s, "examples")
		if len(ex) > 0 {
			s["example"] = ex[0]
		}
	}
	for _, bound := range []string{"Minimum", "Maximum"} {
		key := "exclusive" + bound
		if v, ok := s[key]; ok {
			if _, isBool := v.(bool); !isBool {
				s[strings.ToLower(bound)] = v
				s[key] = true
			}
		}
	}
}

// schemasOf calls f on every schema of the document: components and all
// schema fields of paths.
func schemasOf(doc map[string]any, components map[string]any, f func(map[string]any)) {
	for _, s := range components {
		eachSchema(obj(s), f)
	}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				if k == "schema" {
					eachSchema(obj(child), f)
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc["paths"])
}

func toOpenAPI30(doc map[string]any) map[string]any {
	doc["openapi"] = "3.0.3"
	if l := obj(obj(doc["info"])["license"]); l != nil {
		delete(l, "identifier") // 3.1 only
	}
	schemasOf(doc, obj(obj(doc["components"])["schemas"]), func(s map[string]any) { downgradeSchema(s, "nullable") })
	return doc
}

const (
	refComponents  = "#/components/schemas/"
	refDefinitions = "#/definitions/"
)

func toSwagger20(doc map[string]any) map[string]any {
	components := obj(doc["components"])
	out := map[string]any{"swagger": "2.0", "info": doc["info"]}
	if l := obj(obj(out["info"])["license"]); l != nil {
		delete(l, "identifier")
	}
	if tags := doc["tags"]; tags != nil {
		out["tags"] = tags
	}
	if servers := arr(doc["servers"]); len(servers) > 0 {
		if u, err := url.Parse(str(obj(servers[0])["url"])); err == nil {
			if u.Host != "" {
				out["host"] = u.Host
				out["schemes"] = []any{u.Scheme}
			}
			if u.Path != "" {
				out["basePath"] = u.Path
			}
		}
	}

	definitions := obj(components["schemas"])
	fix := func(s map[string]any) {
		downgradeSchema(s, "x-nullable")
		if s["format"] == "binary" && s["type"] == "string" {
			s["type"] = "file"
			delete(s, "format")
		}
	}
	schemasOf(doc, definitions, fix)
	out["definitions"] = definitions

	if schemes := obj(components["securitySchemes"]); len(schemes) > 0 {
		defs := map[string]any{}
		for name, v := range schemes {
			defs[name] = securityDefinition(obj(v))
		}
		out["securityDefinitions"] = defs
	}

	paths := map[string]any{}
	for p, item := range obj(doc["paths"]) {
		ops := map[string]any{}
		for method, op := range obj(item) {
			ops[method] = swaggerOperation(obj(op))
		}
		paths[p] = ops
	}
	out["paths"] = paths
	moveRefs(out)
	return out
}

// moveRefs points every schema reference to #/definitions.
func moveRefs(v any) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if ref, ok := child.(string); ok && k == "$ref" {
				v[k] = strings.Replace(ref, refComponents, refDefinitions, 1)
				continue
			}
			moveRefs(child)
		}
	case []any:
		for _, child := range v {
			moveRefs(child)
		}
	}
}

func securityDefinition(s map[string]any) map[string]any {
	d := map[string]any{}
	desc := str(s["description"])
	switch {
	case s["type"] == "http" && s["scheme"] == "basic":
		d["type"] = "basic"
	case s["type"] == "apiKey":
		d["type"], d["in"], d["name"] = "apiKey", s["in"], s["name"]
	default: // bearer and OpenID Connect: a token in the Authorization header
		d["type"], d["in"], d["name"] = "apiKey", "header", "Authorization"
		desc = strings.TrimSpace("Bearer token. " + desc)
	}
	if desc != "" {
		d["description"] = desc
	}
	return d
}

// inlineSchema copies the schema keys that Swagger 2.0 allows directly on
// non-body parameters and headers.
func inlineSchema(dst, s map[string]any) {
	for _, k := range []string{"type", "format", "enum", "default", "minimum", "maximum", "exclusiveMinimum",
		"exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "minItems", "maxItems", "uniqueItems"} {
		if v, ok := s[k]; ok {
			dst[k] = v
		}
	}
	if items := obj(s["items"]); items != nil {
		inner := map[string]any{}
		inlineSchema(inner, items)
		dst["items"] = inner
	}
	if _, ok := dst["type"]; !ok {
		dst["type"] = "string" // references and untyped values: send as text
	}
}

func swaggerOperation(op map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"tags", "summary", "description", "operationId", "deprecated", "security"} {
		if v, ok := op[k]; ok {
			out[k] = v
		}
	}
	var params []any
	for _, p := range arr(op["parameters"]) {
		p := obj(p)
		if p["in"] == "cookie" {
			continue // not supported by Swagger 2.0
		}
		sp := map[string]any{"name": p["name"], "in": p["in"]}
		for _, k := range []string{"description", "required"} {
			if v, ok := p[k]; ok {
				sp[k] = v
			}
		}
		inlineSchema(sp, obj(p["schema"]))
		if sp["type"] == "array" && p["in"] == "query" {
			sp["collectionFormat"] = "multi"
		}
		params = append(params, sp)
	}
	if body := obj(op["requestBody"]); body != nil {
		content := obj(body["content"])
		var consumes []any
		for _, ct := range sortedKeys(content) {
			consumes = append(consumes, ct)
		}
		out["consumes"] = consumes
		if jsonBody := obj(content["application/json"]); jsonBody != nil {
			bp := map[string]any{"name": "body", "in": "body", "schema": jsonBody["schema"]}
			for _, k := range []string{"description", "required"} {
				if v, ok := body[k]; ok {
					bp[k] = v
				}
			}
			params = append(params, bp)
		} else {
			form := obj(content["multipart/form-data"])
			if form == nil {
				form = obj(content["application/x-www-form-urlencoded"])
			}
			schema := obj(form["schema"])
			required := arr(schema["required"])
			props := obj(schema["properties"])
			for _, name := range sortedKeys(props) {
				fp := map[string]any{"name": name, "in": "formData"}
				prop := obj(props[name])
				if d, ok := prop["description"]; ok {
					fp["description"] = d
				}
				if slices.Contains(required, any(name)) {
					fp["required"] = true
				}
				inlineSchema(fp, prop)
				if fp["type"] == "array" {
					fp["collectionFormat"] = "multi"
				}
				params = append(params, fp)
			}
		}
	}
	if params != nil {
		out["parameters"] = params
	}

	responses := map[string]any{}
	var produces []any
	for code, r := range obj(op["responses"]) {
		r := obj(r)
		sr := map[string]any{"description": r["description"]}
		content := obj(r["content"])
		for _, ct := range sortedKeys(content) {
			if !slices.Contains(produces, any(ct)) {
				produces = append(produces, ct)
			}
			if _, ok := sr["schema"]; !ok {
				sr["schema"] = obj(content[ct])["schema"]
			}
		}
		if headers := obj(r["headers"]); headers != nil {
			sh := map[string]any{}
			for name, h := range headers {
				h := obj(h)
				header := map[string]any{}
				if d, ok := h["description"]; ok {
					header["description"] = d
				}
				inlineSchema(header, obj(h["schema"]))
				sh[name] = header
			}
			sr["headers"] = sh
		}
		responses[code] = sr
	}
	out["responses"] = responses
	if produces != nil {
		slices.SortFunc(produces, func(a, b any) int { return strings.Compare(str(a), str(b)) })
		out["produces"] = produces
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
