package swaggerkit

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

// Severity is the importance of an [Issue].
type Severity string

// Issue severities.
const (
	SeverityError   Severity = "error"   // the document or the API is broken
	SeverityWarning Severity = "warning" // likely a mistake or a security risk
	SeverityInfo    Severity = "info"    // a suggestion to improve the API
)

// Issue is an API quality finding reported by [API.Lint].
type Issue struct {
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	Location string   `json:"location"` // "GET /hives/{hiveId}" or "Hive.Name"
	Message  string   `json:"message"`
}

func (i Issue) String() string {
	return fmt.Sprintf("%s: %s: %s (%s)", i.Severity, i.Location, i.Message, i.Rule)
}

var (
	verbSegment  = regexp.MustCompile(`(?i)^(get|list|fetch|create|add|new|update|edit|set|delete|remove|del)([-_]|$)`)
	kebabSegment = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Lint checks the API for common design and security problems. Mistakes that
// break the API, such as a path parameter without a field, panic at
// registration instead.
func (a *API) Lint() []Issue {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var issues []Issue
	add := func(sev Severity, rule, loc, format string, args ...any) {
		issues = append(issues, Issue{sev, rule, loc, fmt.Sprintf(format, args...)})
	}

	if a.info.Title == "" {
		add(SeverityError, "info-title", "info", "set Info.Title")
	}
	if a.info.Version == "" {
		add(SeverityWarning, "info-version", "info", "set Info.Version, e.g. from the build")
	}
	for _, name := range a.schemeOrder {
		s := a.securitySchemes[name]
		if s.verify == nil {
			add(SeverityWarning, "security-not-enforced", "securitySchemes."+name,
				"scheme has no verify function; make sure a middleware enforces it")
		}
		if s.Type == "apiKey" && s.In == inQuery {
			add(SeverityWarning, "api-key-in-query", "securitySchemes."+name,
				"API keys in query strings end up in logs and browser history; use a header")
		}
	}

	for _, rt := range a.routes {
		if rt.cfg.hidden {
			continue
		}
		loc := rt.method + " " + docPath(rt.path)
		segments := strings.Split(strings.Trim(rt.path, "/"), "/")
		for _, seg := range segments {
			if m := pathParamPattern.FindStringSubmatch(seg); m != nil {
				if strings.ContainsAny(m[1], "_") || (m[1] != "" && m[1][0] >= 'A' && m[1][0] <= 'Z') {
					add(SeverityInfo, "path-param-case", loc, "use lowerCamelCase for {%s}", m[1])
				}
				continue
			}
			if seg != "" && !kebabSegment.MatchString(seg) {
				add(SeverityWarning, "path-case", loc, "use lowercase kebab-case for path segment %q", seg)
			}
			if verbSegment.MatchString(seg) {
				add(SeverityWarning, "verb-in-path", loc,
					"path segment %q is a verb; express the action with the HTTP method, e.g. DELETE /projects/{id}", seg)
			}
		}
		if len(rt.cfg.tags) == 0 {
			add(SeverityInfo, "operation-tags", loc, "add Tags to group the operation in the documentation")
		}
		if rt.method == http.MethodDelete && rt.input.body != nil {
			add(SeverityWarning, "delete-body", loc, "DELETE requests with a body are dropped by many proxies")
		}
		if rt.method == http.MethodPost && rt.cfg.status == 0 && rt.output == outputJSON &&
			!pathParamPattern.MatchString(segments[len(segments)-1]) && !verbSegment.MatchString(segments[len(segments)-1]) {
			add(SeverityInfo, "post-status", loc, "POST that creates a resource should respond with Status(http.StatusCreated)")
		}
		if len(a.schemeOrder) > 0 && len(rt.cfg.security) == 0 && !rt.cfg.public &&
			slices.Contains([]string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}, rt.method) {
			add(SeverityWarning, "unsecured-write", loc, "write operation without Security; add Security or mark it Public")
		}
		if rt.output == outputJSON && a.gen.resolve(rt.outSchema).Type == "array" {
			add(SeverityInfo, "array-response", loc, "a top-level array cannot get pagination fields later; return an object")
		}
		if b := rt.input.body; b != nil {
			a.lintRequestSchema(b.schema, "body", loc, map[string]bool{}, add)
		}
	}

	for _, name := range a.gen.componentsSorted() {
		s := a.gen.components[name]
		if len(s.Properties) == 0 {
			continue
		}
		missing := 0
		for _, p := range s.Properties {
			if p.Description == "" {
				missing++
			}
		}
		if missing > 0 {
			add(SeverityInfo, "field-doc", name, "%d of %d fields have no doc tag", missing, len(s.Properties))
		}
	}
	return append(issues, a.gen.issues...)
}

// lintRequestSchema looks for unbounded input that can be used to exhaust memory or CPU.
func (a *API) lintRequestSchema(s *Schema, path, loc string, seen map[string]bool, add func(Severity, string, string, string, ...any)) {
	if s == nil {
		return
	}
	if s.Ref != "" {
		if seen[s.Ref] {
			return
		}
		seen[s.Ref] = true
		a.lintRequestSchema(a.gen.components[s.Ref], path, loc, seen, add)
		return
	}
	switch s.Type {
	case "array":
		if s.MaxItems == nil {
			add(SeverityInfo, "unbounded-array", loc, "%s has no max rule; limit the number of items", path)
		}
		a.lintRequestSchema(s.Items, path+"[]", loc, seen, add)
	case "string":
		if s.MaxLength == nil && len(s.Enum) == 0 && s.Format == "" {
			add(SeverityInfo, "unbounded-string", loc, "%s has no max rule; limit the length", path)
		}
	case "object":
		for _, name := range s.propertyNames() {
			a.lintRequestSchema(s.Properties[name], path+"."+name, loc, seen, add)
		}
		if s.AdditionalProperties != nil {
			if s.MaxProperties == nil {
				add(SeverityInfo, "unbounded-map", loc, "%s has no max rule; limit the number of entries", path)
			}
			a.lintRequestSchema(s.AdditionalProperties, path+"{}", loc, seen, add)
		}
	}
}
