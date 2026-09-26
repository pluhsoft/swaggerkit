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

// OpenAPIVersion is the version of the generated documents.
const OpenAPIVersion = "3.1.0"

// OpenAPI 3.1 document. Maps are encoded with sorted keys, so the output is stable.
type document struct {
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Servers    []Server            `json:"servers,omitempty"`
	Tags       []docTag            `json:"tags,omitempty"`
	Paths      map[string]pathItem `json:"paths"`
	Components components          `json:"components"`
}

type docTag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type pathItem map[string]*operation // lowercase method → operation

type operation struct {
	Tags        []string              `json:"tags,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationID string                `json:"operationId"`
	Parameters  []parameter           `json:"parameters,omitempty"`
	RequestBody *requestBody          `json:"requestBody,omitempty"`
	Responses   map[string]response   `json:"responses"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Deprecated  bool    `json:"deprecated,omitempty"`
	Schema      *Schema `json:"schema"`
}

type requestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]mediaType `json:"content"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type mediaType struct {
	Schema *Schema `json:"schema"`
}

type components struct {
	Schemas         map[string]*Schema        `json:"schemas"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

// OpenAPI returns the OpenAPI document as indented JSON.
func (a *API) OpenAPI() ([]byte, error) {
	a.mu.RLock()
	cached := a.spec
	a.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.spec != nil {
		return a.spec, nil
	}
	raw, err := marshalJSON(a.document())
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	a.spec = buf.Bytes()
	return a.spec, nil
}

// WriteOpenAPI writes the OpenAPI document to w, for example to generate
// openapi.json during development.
func (a *API) WriteOpenAPI(w io.Writer) error {
	doc, err := a.OpenAPI()
	if err != nil {
		return err
	}
	_, err = w.Write(doc)
	return err
}

// OpenAPIHandler returns a handler that serves the OpenAPI document.
func (a *API) OpenAPIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc, err := a.OpenAPI()
		if err != nil {
			a.writeError(r.Context(), w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(doc)
	})
}

// document builds the document. The caller holds a.mu.
func (a *API) document() document {
	info := a.info
	if info.Version == "" {
		info.Version = "0.0.0"
	}
	doc := document{
		OpenAPI: OpenAPIVersion,
		Info:    info,
		Paths:   map[string]pathItem{},
		Components: components{
			Schemas:         map[string]*Schema{},
			SecuritySchemes: a.securitySchemes,
		},
	}
	for _, s := range a.servers {
		doc.Servers = append(doc.Servers, Server{strings.TrimSuffix(s.URL, "/") + a.basePath, s.Description})
	}
	if len(doc.Servers) == 0 && a.basePath != "" {
		doc.Servers = []Server{{URL: a.basePath}}
	}

	// A user type may already be called Problem.
	problem := "Problem"
	for i := 2; a.gen.components[problem] != nil; i++ {
		problem = "Problem" + strconv.Itoa(i)
	}
	for name, s := range a.gen.components {
		doc.Components.Schemas[name] = s
	}
	doc.Components.Schemas[problem] = problemSchema()

	doc.Tags = slices.Clone(a.tags)
	for _, rt := range a.routes {
		if rt.cfg.hidden {
			continue
		}
		path := docPath(rt.path)
		if doc.Paths[path] == nil {
			doc.Paths[path] = pathItem{}
		}
		doc.Paths[path][strings.ToLower(rt.method)] = a.operation(rt, problem)
		for _, t := range rt.cfg.tags {
			if !slices.ContainsFunc(doc.Tags, func(d docTag) bool { return d.Name == t }) {
				doc.Tags = append(doc.Tags, docTag{Name: t})
			}
		}
	}
	return doc
}

func (a *API) operation(rt *route, problem string) *operation {
	op := &operation{
		Tags:        rt.cfg.tags,
		Summary:     rt.summary,
		Description: rt.cfg.description,
		OperationID: rt.operationID,
		Responses:   map[string]response{},
		Deprecated:  rt.cfg.deprecated,
	}
	for _, p := range rt.input.params {
		s := p.schema.clone()
		param := parameter{Name: p.name, In: p.in, Description: s.Description, Required: p.required, Deprecated: s.Deprecated, Schema: s}
		s.Description, s.Deprecated = "", false
		op.Parameters = append(op.Parameters, param)
	}
	if b := rt.input.body; b != nil {
		op.RequestBody = &requestBody{
			Description: b.description,
			Required:    b.required,
			Content:     map[string]mediaType{"application/json": {b.schema}},
		}
	}

	if form := rt.input.form; len(form) > 0 {
		schema := formSchema(form)
		body := &requestBody{Required: len(schema.Required) > 0, Content: map[string]mediaType{"multipart/form-data": {schema}}}
		if !formHasFiles(form) {
			body.Content["application/x-www-form-urlencoded"] = mediaType{schema}
		}
		op.RequestBody = body
	}

	status := successStatus(rt)
	success := response{Description: http.StatusText(status)}
	switch rt.output {
	case outputJSON:
		success.Content = map[string]mediaType{"application/json": {rt.outSchema}}
	case outputFile:
		ct := rt.cfg.produces
		if ct == "" {
			ct = "application/octet-stream"
		}
		success.Content = map[string]mediaType{ct: {&Schema{Type: "string", Format: "binary"}}}
	}
	op.Responses[strconv.Itoa(status)] = success
	for _, code := range rt.cfg.statuses {
		extra := success
		extra.Description = http.StatusText(code)
		op.Responses[strconv.Itoa(code)] = extra
	}

	codes := slices.Clone(rt.cfg.errors)
	if len(rt.cfg.security) > 0 {
		codes = append(codes, http.StatusUnauthorized)
	}
	if !rt.input.empty() {
		codes = append(codes, http.StatusUnprocessableEntity)
	}
	problemContent := map[string]mediaType{"application/problem+json": {&Schema{Ref: problem}}}
	for _, code := range codes {
		op.Responses[strconv.Itoa(code)] = response{Description: http.StatusText(code), Content: problemContent}
	}
	op.Responses["default"] = response{Description: "Error", Content: problemContent}

	for _, name := range rt.cfg.security {
		op.Security = append(op.Security, map[string][]string{name: {}})
	}
	return op
}
