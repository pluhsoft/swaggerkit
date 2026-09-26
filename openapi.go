package swaggerkit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// OpenAPIVersion selects the OpenAPI version of a generated document.
type OpenAPIVersion string

// Supported OpenAPI versions.
const (
	OpenAPI31 OpenAPIVersion = "3.1.0" // default; JSON Schema 2020-12
	OpenAPI30 OpenAPIVersion = "3.0.3" // for tools without 3.1 support
)

// OpenAPI returns the OpenAPI document as indented JSON.
func (a *API) OpenAPI(v OpenAPIVersion) ([]byte, error) {
	if v != OpenAPI30 {
		v = OpenAPI31
	}
	a.mu.RLock()
	cached := a.specCache[v]
	a.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cached := a.specCache[v]; cached != nil {
		return cached, nil
	}
	raw, err := marshalJSON(a.document(v))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	a.specCache[v] = buf.Bytes()
	return buf.Bytes(), nil
}

// WriteOpenAPI writes the OpenAPI document to w, for example to generate
// openapi.json during development.
func (a *API) WriteOpenAPI(w io.Writer, v OpenAPIVersion) error {
	doc, err := a.OpenAPI(v)
	if err != nil {
		return err
	}
	_, err = w.Write(doc)
	return err
}

// OpenAPIHandler returns a handler that serves the OpenAPI document.
func (a *API) OpenAPIHandler(v OpenAPIVersion) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc, err := a.OpenAPI(v)
		if err != nil {
			a.writeError(r.Context(), w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(doc)
	})
}

var methodOrder = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// document builds the document. The caller holds a.mu.
func (a *API) document(v OpenAPIVersion) orderedMap {
	problem := "Problem"
	for i := 2; a.gen.components[problem] != nil; i++ {
		problem = "Problem" + strconv.Itoa(i)
	}

	doc := orderedMap{{"openapi", string(v)}, {"info", a.infoDocument(v)}}
	if servers := a.serversDocument(); len(servers) > 0 {
		doc.set("servers", servers)
	}

	var tags []string
	paths := map[string]orderedMap{}
	var pathOrder []string
	for _, rt := range a.routes {
		if rt.cfg.hidden {
			continue
		}
		p := docPath(rt.path)
		if _, ok := paths[p]; !ok {
			pathOrder = append(pathOrder, p)
		}
		item := paths[p]
		item.set(strings.ToLower(rt.method), a.operationDocument(rt, v, problem))
		paths[p] = item
		for _, t := range rt.cfg.tags {
			if !slices.Contains(tags, t) {
				tags = append(tags, t)
			}
		}
	}
	if tagDoc := a.tagsDocument(tags); len(tagDoc) > 0 {
		doc.set("tags", tagDoc)
	}
	slices.Sort(pathOrder)
	var pathsDoc orderedMap
	for _, p := range pathOrder {
		item := paths[p]
		slices.SortStableFunc(item, func(x, y keyValue) int {
			return slices.Index(methodOrder, x.Key) - slices.Index(methodOrder, y.Key)
		})
		pathsDoc.set(p, item)
	}
	if pathsDoc == nil {
		pathsDoc = orderedMap{}
	}
	doc.set("paths", pathsDoc)

	var schemas orderedMap
	for _, name := range a.gen.componentsSorted() {
		schemas.set(name, a.gen.components[name].document(v))
	}
	schemas.set(problem, problemSchema().document(v))
	components := orderedMap{{"schemas", schemas}}
	if len(a.schemeOrder) > 0 {
		var schemes orderedMap
		for _, name := range a.schemeOrder {
			schemes.set(name, a.securitySchemes[name].document())
		}
		components.set("securitySchemes", schemes)
	}
	doc.set("components", components)
	return doc
}

func (a *API) infoDocument(v OpenAPIVersion) orderedMap {
	info := orderedMap{{"title", a.info.Title}}
	if a.info.Description != "" {
		info.set("description", a.info.Description)
	}
	if a.info.TermsOfService != "" {
		info.set("termsOfService", a.info.TermsOfService)
	}
	if c := a.info.Contact; c != nil {
		var m orderedMap
		for _, kv := range []keyValue{{"name", c.Name}, {"url", c.URL}, {"email", c.Email}} {
			if kv.Value != "" {
				m.set(kv.Key, kv.Value)
			}
		}
		info.set("contact", m)
	}
	if l := a.info.License; l != nil {
		m := orderedMap{{"name", l.Name}}
		if l.Identifier != "" && v == OpenAPI31 && l.URL == "" {
			m.set("identifier", l.Identifier)
		}
		if l.URL != "" {
			m.set("url", l.URL)
		}
		info.set("license", m)
	}
	version := a.info.Version
	if version == "" {
		version = "0.0.0"
	}
	info.set("version", version)
	return info
}

func (a *API) serversDocument() []orderedMap {
	var out []orderedMap
	for _, s := range a.servers {
		m := orderedMap{{"url", strings.TrimSuffix(s.URL, "/") + a.basePath}}
		if s.Description != "" {
			m.set("description", s.Description)
		}
		out = append(out, m)
	}
	if len(out) == 0 && a.basePath != "" {
		out = append(out, orderedMap{{"url", a.basePath}})
	}
	return out
}

func (a *API) tagsDocument(used []string) []orderedMap {
	var out []orderedMap
	var names []string
	for _, t := range a.tags {
		m := orderedMap{{"name", t.name}}
		if t.description != "" {
			m.set("description", t.description)
		}
		out = append(out, m)
		names = append(names, t.name)
	}
	for _, t := range used {
		if !slices.Contains(names, t) {
			out = append(out, orderedMap{{"name", t}})
		}
	}
	return out
}

func (a *API) operationDocument(rt *route, v OpenAPIVersion, problem string) orderedMap {
	op := orderedMap{}
	if len(rt.cfg.tags) > 0 {
		op.set("tags", rt.cfg.tags)
	}
	if rt.summary != "" {
		op.set("summary", rt.summary)
	}
	if rt.cfg.description != "" {
		op.set("description", rt.cfg.description)
	}
	op.set("operationId", rt.operationID)

	var params []orderedMap
	for _, p := range rt.input.params {
		m := orderedMap{{"name", p.name}, {"in", p.in}}
		s := p.schema.clone()
		if s.Description != "" {
			m.set("description", s.Description)
			s.Description = ""
		}
		if p.required {
			m.set("required", true)
		}
		if s.Deprecated {
			m.set("deprecated", true)
			s.Deprecated = false
		}
		m.set("schema", s.document(v))
		params = append(params, m)
	}
	if len(params) > 0 {
		op.set("parameters", params)
	}
	if b := rt.input.body; b != nil {
		body := orderedMap{}
		if b.description != "" {
			body.set("description", b.description)
		}
		if b.required {
			body.set("required", true)
		}
		body.set("content", orderedMap{{"application/json", orderedMap{{"schema", b.schema.document(v)}}}})
		op.set("requestBody", body)
	}

	responses := orderedMap{}
	status := successStatus(rt)
	success := orderedMap{{"description", http.StatusText(status)}}
	switch rt.output {
	case outputJSON:
		success.set("content", orderedMap{{"application/json", orderedMap{{"schema", rt.outSchema.document(v)}}}})
	case outputFile:
		ct := rt.cfg.produces
		if ct == "" {
			ct = "application/octet-stream"
		}
		success.set("content", orderedMap{{ct, orderedMap{{"schema", orderedMap{{"type", "string"}, {"format", "binary"}}}}}})
	}
	responses.set(strconv.Itoa(status), success)

	codes := slices.Clone(rt.cfg.errors)
	if len(rt.cfg.security) > 0 {
		codes = append(codes, http.StatusUnauthorized)
	}
	if !rt.input.empty() {
		codes = append(codes, http.StatusUnprocessableEntity)
	}
	slices.Sort(codes)
	problemRef := orderedMap{{"application/problem+json", orderedMap{{"schema", (&Schema{Ref: problem}).document(v)}}}}
	for _, code := range slices.Compact(codes) {
		responses.set(strconv.Itoa(code), orderedMap{{"description", http.StatusText(code)}, {"content", problemRef}})
	}
	responses.set("default", orderedMap{{"description", "Error"}, {"content", problemRef}})
	op.set("responses", responses)

	if rt.cfg.deprecated {
		op.set("deprecated", true)
	}
	if len(rt.cfg.security) > 0 {
		var reqs []orderedMap
		for _, name := range rt.cfg.security {
			reqs = append(reqs, orderedMap{{name, []string{}}})
		}
		op.set("security", reqs)
	}
	return op
}
