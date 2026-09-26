package swaggerkit

import (
	"bytes"
	"html/template"
	"net/http"
)

// Swagger UI is loaded from jsDelivr, pinned to a version and checked with
// Subresource Integrity, so the module has no embedded assets.
const (
	swaggerUIVersion = "5.33.0"
	swaggerUICSS     = "https://cdn.jsdelivr.net/npm/swagger-ui-dist@" + swaggerUIVersion + "/swagger-ui.css"
	swaggerUICSSSRI  = "sha384-Ov4/wv3j2bmct8cDc5X4ngJZohVPzEmc6uDPH8WeljUxO5vtoykvMEfbu9Vh6RaW"
	swaggerUIJS      = "https://cdn.jsdelivr.net/npm/swagger-ui-dist@" + swaggerUIVersion + "/swagger-ui-bundle.js"
	swaggerUIJSSRI   = "sha384-YDALVcy8kj8yltLBVi1vBiBAUqdxvus673gM8XKwiy6aDUJFXivF/KCufekjYbVf"
)

var swaggerUIPage = template.Must(template.New("ui").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="{{.CSS}}" integrity="{{.CSSSRI}}" crossorigin="anonymous">
</head>
<body>
<div id="swagger-ui"></div>
<script src="{{.JS}}" integrity="{{.JSSRI}}" crossorigin="anonymous"></script>
<script>
window.ui = SwaggerUIBundle({url: {{.SpecURL}}, dom_id: "#swagger-ui", deepLinking: true});
</script>
</body>
</html>
`))

// registerDocs serves Swagger UI and the OpenAPI documents under a.docsPath.
func (a *API) registerDocs() {
	base := a.basePath + a.docsPath
	if a.docsPath == "/" {
		base = a.basePath
	}
	var page bytes.Buffer
	err := swaggerUIPage.Execute(&page, map[string]string{
		"Title":   a.info.Title,
		"CSS":     swaggerUICSS,
		"CSSSRI":  swaggerUICSSSRI,
		"JS":      swaggerUIJS,
		"JSSRI":   swaggerUIJSSRI,
		"SpecURL": base + "/openapi.json",
	})
	if err != nil {
		panic("swaggerkit: render Swagger UI page: " + err.Error())
	}
	uiPath := base
	if uiPath == "" {
		uiPath = "/{$}"
	}
	a.mux.HandleFunc("GET "+uiPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(page.Bytes())
	})
	a.mux.Handle("GET "+base+"/openapi.json", a.OpenAPIHandler())
	a.mux.Handle("GET "+base+"/openapi-3.0.json", a.DocumentHandler(FormatOpenAPI30))
	a.mux.Handle("GET "+base+"/swagger.json", a.DocumentHandler(FormatSwagger20))
}
