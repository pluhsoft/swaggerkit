package swaggerkit

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"unicode"
)

// HandlerFunc is a typed handler. In describes the request: fields tagged
// path, query, header and cookie are parameters, a field named Body is the
// JSON body. If In has no such fields, the whole In is the body.
// Out is encoded as the JSON response; see also [NoContent] and [File].
type HandlerFunc[In, Out any] func(ctx context.Context, in In) (Out, error)

// RouteOption configures a route or a [Group].
type RouteOption func(*routeConfig)

type routeConfig struct {
	summary      string
	description  string
	operationID  string
	tags         []string
	deprecated   bool
	hidden       bool
	status       int
	security     []string
	public       bool
	middlewares  []Middleware
	errors       []int
	produces     string
	maxBodyBytes int64
}

// Summary sets the short summary of the operation.
// By default it is derived from the handler name: ListHives → "List hives".
func Summary(s string) RouteOption { return func(c *routeConfig) { c.summary = s } }

// Description sets the description of the operation. Markdown is allowed.
func Description(s string) RouteOption { return func(c *routeConfig) { c.description = s } }

// OperationID sets the operation ID. By default it is derived from the handler
// name: ListHives → "listHives". Operation IDs must be unique.
func OperationID(id string) RouteOption { return func(c *routeConfig) { c.operationID = id } }

// Tags adds tags that group operations in the documentation.
func Tags(tags ...string) RouteOption {
	return func(c *routeConfig) { c.tags = append(c.tags, tags...) }
}

// Deprecated marks the operation as deprecated.
func Deprecated() RouteOption { return func(c *routeConfig) { c.deprecated = true } }

// Hidden excludes the operation from the OpenAPI document.
func Hidden() RouteOption { return func(c *routeConfig) { c.hidden = true } }

// Status sets the success status code. The default is 200, or 204 for [NoContent].
func Status(code int) RouteOption { return func(c *routeConfig) { c.status = code } }

// Security documents that the route requires one of the named security
// schemes, registered with [WithSecurityScheme]. Several names mean any of
// them is accepted. Check credentials in a middleware passed with [Middlewares].
func Security(schemes ...string) RouteOption {
	return func(c *routeConfig) { c.security, c.public = slices.Clone(schemes), false }
}

// Public removes security requirements inherited from a group.
func Public() RouteOption {
	return func(c *routeConfig) { c.security, c.public = nil, true }
}

// Middlewares adds middlewares for the route or group, e.g. authentication.
// They run after the API middlewares and before request validation.
func Middlewares(mw ...Middleware) RouteOption {
	return func(c *routeConfig) { c.middlewares = append(c.middlewares, mw...) }
}

// Errors documents error status codes that the handler returns, e.g. 404, 409.
// 401 for routes with [Security], 422 for routes with input and a default
// error response are documented automatically.
func Errors(codes ...int) RouteOption {
	return func(c *routeConfig) { c.errors = append(c.errors, codes...) }
}

// MaxBodyBytes overrides [WithMaxBodyBytes] for the route, e.g. for uploads.
func MaxBodyBytes(n int64) RouteOption { return func(c *routeConfig) { c.maxBodyBytes = n } }

// Produces sets the content type of a [File] response in the documentation,
// e.g. "image/png". The default is "application/octet-stream".
func Produces(contentType string) RouteOption {
	return func(c *routeConfig) { c.produces = contentType }
}

// route is a registered operation.
type route struct {
	method      string
	path        string // path in the OpenAPI document, without the base path
	cfg         routeConfig
	input       *inputPlan
	output      outputKind
	outSchema   *Schema
	summary     string // explicit or derived
	operationID string
	handlerName string
}

// Get registers a GET route.
func Get[In, Out any](r Router, path string, h HandlerFunc[In, Out], opts ...RouteOption) {
	Handle(r, http.MethodGet, path, h, opts...)
}

// Post registers a POST route.
func Post[In, Out any](r Router, path string, h HandlerFunc[In, Out], opts ...RouteOption) {
	Handle(r, http.MethodPost, path, h, opts...)
}

// Put registers a PUT route.
func Put[In, Out any](r Router, path string, h HandlerFunc[In, Out], opts ...RouteOption) {
	Handle(r, http.MethodPut, path, h, opts...)
}

// Patch registers a PATCH route.
func Patch[In, Out any](r Router, path string, h HandlerFunc[In, Out], opts ...RouteOption) {
	Handle(r, http.MethodPatch, path, h, opts...)
}

// Delete registers a DELETE route.
func Delete[In, Out any](r Router, path string, h HandlerFunc[In, Out], opts ...RouteOption) {
	Handle(r, http.MethodDelete, path, h, opts...)
}

var allowedMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace,
}

// Handle registers a route. Path uses net/http patterns: "/hives/{hiveId}",
// "/files/{path...}". Mistakes in the route or its types, such as a path
// parameter without a matching field, panic, as http.ServeMux does.
func Handle[In, Out any](r Router, method, path string, h HandlerFunc[In, Out], opts ...RouteOption) {
	if h == nil {
		panic("swaggerkit: nil handler for " + method + " " + path)
	}
	g := r.base()
	a := g.api
	method = strings.ToUpper(method)
	if !slices.Contains(allowedMethods, method) {
		panic(fmt.Sprintf("swaggerkit: unsupported method %q", method))
	}
	cfg := routeConfig{}
	for _, opt := range g.opts {
		opt(&cfg)
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	fullPath := g.prefix + "/" + strings.TrimPrefix(path, "/")
	if fullPath != "/" {
		fullPath = strings.TrimSuffix(fullPath, "/")
	}
	where := method + " " + fullPath
	pathParams, err := parsePath(fullPath)
	if err != nil {
		panic("swaggerkit: " + where + ": " + err.Error())
	}
	for _, s := range cfg.security {
		if _, ok := a.securitySchemes[s]; !ok {
			panic(fmt.Sprintf("swaggerkit: %s: unknown security scheme %q; register it with WithSecurityScheme", where, s))
		}
	}
	if cfg.status != 0 && (cfg.status < 100 || cfg.status > 399) {
		panic(fmt.Sprintf("swaggerkit: %s: success status %d must be 1xx-3xx", where, cfg.status))
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	rt := &route{method: method, path: fullPath, cfg: cfg, handlerName: funcName(h)}
	withSchemaErrors(where, func() {
		rt.input = analyzeInput(a.gen, reflect.TypeFor[In](), method, pathParams, where)
		rt.output, rt.outSchema = analyzeOutput(a.gen, reflect.TypeFor[Out](), where)
	})
	rt.summary = cfg.summary
	if rt.summary == "" {
		rt.summary = humanize(rt.handlerName)
	}
	rt.operationID = cfg.operationID
	if rt.operationID == "" {
		rt.operationID = lowerFirst(rt.handlerName)
		if rt.operationID == "" {
			rt.operationID = pathOperationID(method, fullPath)
		}
	}
	for _, other := range a.routes {
		if other.operationID == rt.operationID {
			panic(fmt.Sprintf("swaggerkit: %s: operation ID %q is already used by %s %s; set OperationID", where, rt.operationID, other.method, other.path))
		}
	}

	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		serve(a, rt, w, req, h)
	})
	for i := len(cfg.middlewares) - 1; i >= 0; i-- {
		handler = cfg.middlewares[i](handler)
	}
	muxPath := a.basePath + fullPath
	if fullPath == "/" {
		muxPath = a.basePath + "/{$}"
	}
	a.mux.Handle(method+" "+muxPath, handler)
	a.routes = append(a.routes, rt)
	a.spec = nil
}

// withSchemaErrors converts schema errors into panics that name the route.
func withSchemaErrors(where string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*schemaError); ok {
				panic(fmt.Sprintf("swaggerkit: %s: %s: %s", where, e.where, e.msg))
			}
			panic(r)
		}
	}()
	f()
}

var (
	pathParamPattern = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)(\.\.\.)?\}$`)
	closureName      = regexp.MustCompile(`^func\d+$`)
	wordPattern      = regexp.MustCompile(`[A-Za-z0-9]+`)
)

// parsePath checks a route path and returns the names of its parameters.
func parsePath(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path must start with /")
	}
	if strings.Contains(path, "//") {
		return nil, fmt.Errorf("path must not contain //")
	}
	var names []string
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, seg := range segments {
		if !strings.ContainsAny(seg, "{}") {
			continue
		}
		m := pathParamPattern.FindStringSubmatch(seg)
		if m == nil {
			return nil, fmt.Errorf("segment %q: parameters must be whole segments like {hiveId}; regular expressions are not supported", seg)
		}
		if m[2] != "" && i != len(segments)-1 {
			return nil, fmt.Errorf("{%s...} must be the last segment", m[1])
		}
		if slices.Contains(names, m[1]) {
			return nil, fmt.Errorf("duplicate parameter {%s}", m[1])
		}
		names = append(names, m[1])
	}
	return names, nil
}

// docPath converts a net/http pattern path to an OpenAPI path.
func docPath(path string) string { return strings.ReplaceAll(path, "...}", "}") }

// funcName returns the Go name of a function: "ListHives" for
// "example.com/apiary.ListHives" or "apiary.(*Store).ListHives-fm", and ""
// for anonymous functions.
func funcName(fn any) string {
	f := runtime.FuncForPC(reflect.ValueOf(fn).Pointer())
	if f == nil {
		return ""
	}
	name := f.Name()
	name = strings.ReplaceAll(name, "[...]", "")
	name = name[strings.LastIndex(name, "/")+1:]
	name = strings.TrimSuffix(name, "-fm")
	name = name[strings.LastIndex(name, ".")+1:]
	if name == "" || !unicode.IsLetter([]rune(name)[0]) || closureName.MatchString(name) {
		return ""
	}
	return name
}

// pathOperationID builds an ID like "getHivesHiveIdHoney".
func pathOperationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, part := range wordPattern.FindAllString(path, -1) {
		b.WriteString(exportedName(part))
	}
	return b.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	words := splitWords(s)
	words[0] = strings.ToLower(words[0])
	return strings.Join(words, "")
}

// humanize turns "GetHiveByID" into "Get hive by ID".
func humanize(name string) string {
	words := splitWords(name)
	for i, w := range words {
		if i > 0 && !isAcronym(w) {
			words[i] = strings.ToLower(w)
		}
	}
	return strings.Join(words, " ")
}

func isAcronym(w string) bool { return len(w) > 1 && strings.ToUpper(w) == w }

// splitWords splits a Go identifier into words: "GetHiveByID" → Get, Hive, By, ID.
func splitWords(s string) []string {
	var words []string
	r := []rune(s)
	start := 0
	for i := 1; i < len(r); i++ {
		prevLower := unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1])
		upper := unicode.IsUpper(r[i])
		nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
		if upper && (prevLower || (unicode.IsUpper(r[i-1]) && nextLower)) {
			words = append(words, string(r[start:i]))
			start = i
		}
	}
	if start < len(r) {
		words = append(words, string(r[start:]))
	}
	return words
}
