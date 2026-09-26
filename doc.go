// Package swaggerkit builds typed HTTP APIs on net/http: it binds and
// validates requests from Go types and generates the OpenAPI (Swagger)
// document from the same types, so the documentation always matches the code.
//
// A handler takes a context and an input struct and returns an output value:
//
//	type GetHiveInput struct {
//		HiveID int `path:"hiveId" validate:"min=1"`
//	}
//
//	func GetHive(ctx context.Context, in GetHiveInput) (Hive, error) {
//		hive, ok := store.Get(in.HiveID)
//		if !ok {
//			return Hive{}, swaggerkit.NotFound("hive not found")
//		}
//		return hive, nil
//	}
//
//	api := swaggerkit.New(swaggerkit.Info{Title: "Apiary", Version: "1.0.0"},
//		swaggerkit.WithDocs("/docs"))
//	swaggerkit.Get(api, "/hives/{hiveId}", GetHive, swaggerkit.Tags("hives"))
//	http.ListenAndServe(":8080", api)
//
// Input fields tagged path, query, header and cookie are parameters; a field
// named Body is the JSON request body. Fields are validated with the validate
// tag and documented with the doc and example tags. Errors are sent as RFC 9457
// problem details.
//
// The API serves Swagger UI and the OpenAPI 3.1 document ([WithDocs]),
// writes the document to a file ([API.WriteOpenAPI]) and reports design
// problems ([API.Lint]).
//
// Guides: https://pluhsoft.github.io/swaggerkit/
package swaggerkit
