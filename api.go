package swaggerkit

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// Info describes the API in the OpenAPI document.
type Info struct {
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	TermsOfService string   `json:"termsOfService,omitempty"`
	Contact        *Contact `json:"contact,omitempty"`
	License        *License `json:"license,omitempty"`
	Version        string   `json:"version"`
}

// Contact is the API contact in the OpenAPI document.
type Contact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// License is the API license in the OpenAPI document.
type License struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier,omitempty"` // SPDX identifier, e.g. "MIT"; use it or URL
	URL        string `json:"url,omitempty"`
}

// Server is an API server in the OpenAPI document.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Middleware wraps an http.Handler. Standard net/http middlewares fit as is.
type Middleware = func(http.Handler) http.Handler

// API is an http.Handler that serves typed handlers and generates their
// OpenAPI document. Create it with [New] and add routes with [Get], [Post],
// [Put], [Patch], [Delete] or [Handle].
//
// An API is safe for concurrent use.
type API struct {
	info            Info
	servers         []Server
	basePath        string
	logger          *slog.Logger
	maxBodyBytes    int64
	strict          bool
	tags            []docTag
	securitySchemes map[string]SecurityScheme
	schemeOrder     []string
	docsPath        string

	mu          sync.RWMutex
	mux         *http.ServeMux
	handler     http.Handler // mux wrapped in global middlewares
	middlewares []Middleware
	routes      []*route
	gen         *schemaGen
	spec        []byte            // cached OpenAPI 3.1 document
	specs       map[Format][]byte // cached converted documents
	root        *Group
}

// Option configures an [API].
type Option func(*API)

// WithServer adds a server to the OpenAPI document. Without servers the
// document uses the base path, e.g. "/api/v1".
func WithServer(url, description string) Option {
	return func(a *API) { a.servers = append(a.servers, Server{URL: url, Description: description}) }
}

// WithBasePath serves all routes under prefix, e.g. "/api/v1". Paths in the
// OpenAPI document stay relative to it; server URLs get the prefix appended.
func WithBasePath(prefix string) Option {
	return func(a *API) { a.basePath = "/" + strings.Trim(prefix, "/") }
}

// WithLogger sets where server errors are logged: handler errors that become
// 500, and panics. Nothing else is logged. The default is slog.Default();
// nil turns logging off. Records carry the request context, so a
// slog.Handler can add request fields such as a request ID.
func WithLogger(l *slog.Logger) Option {
	return func(a *API) { a.logger = l }
}

// WithMaxBodyBytes limits the size of request bodies. The default is 1 MiB.
func WithMaxBodyBytes(n int64) Option {
	return func(a *API) { a.maxBodyBytes = n }
}

// WithUnknownFields accepts JSON fields that the Go types do not declare.
// By default they are rejected with 422.
func WithUnknownFields() Option {
	return func(a *API) { a.strict = false }
}

// WithTag adds a description to a tag in the OpenAPI document.
// Tags are listed in the order they are added.
func WithTag(name, description string) Option {
	return func(a *API) { a.tags = append(a.tags, docTag{name, description}) }
}

// WithSecurityScheme registers a security scheme for the OpenAPI document.
// Routes require it with the [Security] option.
func WithSecurityScheme(name string, scheme SecurityScheme) Option {
	return func(a *API) {
		if _, ok := a.securitySchemes[name]; !ok {
			a.schemeOrder = append(a.schemeOrder, name)
		}
		a.securitySchemes[name] = scheme
	}
}

// WithDocs serves Swagger UI at path and the documents at path+"/openapi.json"
// (OpenAPI 3.1), path+"/openapi-3.0.json" and path+"/swagger.json" (Swagger 2.0).
func WithDocs(path string) Option {
	return func(a *API) { a.docsPath = "/" + strings.Trim(path, "/") }
}

const defaultMaxBodyBytes = 1 << 20

// New returns an API.
func New(info Info, opts ...Option) *API {
	a := &API{
		info:            info,
		logger:          slog.Default(),
		maxBodyBytes:    defaultMaxBodyBytes,
		strict:          true,
		securitySchemes: map[string]SecurityScheme{},
		mux:             http.NewServeMux(),
		gen:             newSchemaGen(),
		specs:           map[Format][]byte{},
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.basePath == "/" {
		a.basePath = ""
	}
	a.root = &Group{api: a}
	a.handler = a.mux
	if a.docsPath != "" {
		a.registerDocs()
	}
	return a
}

// ServeHTTP implements http.Handler.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	h := a.handler
	a.mu.RUnlock()
	h.ServeHTTP(w, r)
}

// Use adds middlewares that run for every request, including unmatched
// routes and CORS preflight requests. The first middleware is the outermost.
func (a *API) Use(mw ...Middleware) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.middlewares = append(a.middlewares, mw...)
	var h http.Handler = a.mux
	for i := len(a.middlewares) - 1; i >= 0; i-- {
		h = a.middlewares[i](h)
	}
	a.handler = h
}

// Group returns a group of routes under prefix that share options,
// e.g. tags, security and middlewares.
func (a *API) Group(prefix string, opts ...RouteOption) *Group {
	return a.root.Group(prefix, opts...)
}

func (a *API) base() *Group { return a.root }

// Router is an [API] or a [Group].
type Router interface {
	base() *Group
}

// Group is a set of routes with a common path prefix and options.
type Group struct {
	api    *API
	prefix string
	opts   []RouteOption
}

// Group returns a nested group.
func (g *Group) Group(prefix string, opts ...RouteOption) *Group {
	p := g.prefix
	if t := strings.Trim(prefix, "/"); t != "" {
		p += "/" + t
	}
	return &Group{api: g.api, prefix: p, opts: append(append([]RouteOption{}, g.opts...), opts...)}
}

func (g *Group) base() *Group { return g }
