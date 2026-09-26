package swaggerkit

// SecurityScheme describes how clients authenticate, for the OpenAPI document.
// Create it with [BearerAuth], [BasicAuth], [APIKeyAuth] or
// [OpenIDConnectAuth], register it with [WithSecurityScheme] and require it on
// routes with [Security].
//
// swaggerkit documents authentication; checking credentials is the job of your
// middleware, passed with [Middlewares] next to [Security].
type SecurityScheme struct {
	Type             string `json:"type"` // "http", "apiKey" or "openIdConnect"
	Description      string `json:"description,omitempty"`
	Scheme           string `json:"scheme,omitempty"`       // "bearer" or "basic" for Type "http"
	BearerFormat     string `json:"bearerFormat,omitempty"` // e.g. "JWT"
	In               string `json:"in,omitempty"`           // "header", "query" or "cookie" for Type "apiKey"
	Name             string `json:"name,omitempty"`         // header, query or cookie name for Type "apiKey"
	OpenIDConnectURL string `json:"openIdConnectUrl,omitempty"`
}

// BearerAuth returns an HTTP bearer scheme ("Authorization: Bearer <token>").
// format is a hint such as "JWT" and may be empty.
func BearerAuth(format string) SecurityScheme {
	return SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: format}
}

// BasicAuth returns an HTTP basic scheme.
func BasicAuth() SecurityScheme {
	return SecurityScheme{Type: "http", Scheme: "basic"}
}

// APIKeyAuth returns an API key scheme. in is "header", "query" or "cookie";
// name is the header, query parameter or cookie name.
func APIKeyAuth(in, name string) SecurityScheme {
	switch in {
	case inHeader, inQuery, inCookie:
	default:
		panic(`swaggerkit: APIKeyAuth: in must be "header", "query" or "cookie"`)
	}
	return SecurityScheme{Type: "apiKey", In: in, Name: name}
}

// OpenIDConnectAuth returns an OpenID Connect scheme. discoveryURL points to
// the provider's /.well-known/openid-configuration.
func OpenIDConnectAuth(discoveryURL string) SecurityScheme {
	return SecurityScheme{Type: "openIdConnect", OpenIDConnectURL: discoveryURL}
}
