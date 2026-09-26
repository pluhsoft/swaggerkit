package main

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pluhsoft/swaggerkit"
)

// Apiary holds the handlers.
type Apiary struct {
	store  *Store
	photos photos
}

type beekeeperKey struct{}

// BeekeeperFrom returns the authenticated beekeeper.
func BeekeeperFrom(ctx context.Context) string {
	name, _ := ctx.Value(beekeeperKey{}).(string)
	return name
}

// requireBeekeeper checks the bearer token. It is an ordinary net/http
// middleware; swaggerkit only documents the scheme.
func requireBeekeeper(token string) swaggerkit.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				swaggerkit.WriteError(w, swaggerkit.Unauthorized("unknown token"))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), beekeeperKey{}, "beekeeper")))
		})
	}
}

// NewAPI wires the routes. token is the bearer token of the beekeeper.
func NewAPI(store *Store, token string, logger *slog.Logger) *swaggerkit.API {
	bearer := swaggerkit.BearerAuth("")
	bearer.Description = "Beekeeper token. The demo token is `beekeeper`."

	api := swaggerkit.New(swaggerkit.Info{
		Title:       "Apiary API",
		Version:     "1.0.0",
		Description: "Manage beehives: count bees, move colonies and harvest honey. An example of swaggerkit.",
		License:     &swaggerkit.License{Name: "MIT", Identifier: "MIT"},
	},
		swaggerkit.WithBasePath("/api/v1"),
		swaggerkit.WithDocs("/docs"),
		swaggerkit.WithLogger(logger),
		swaggerkit.WithSecurityScheme("beekeeper", bearer),
		swaggerkit.WithTag("hives", "Beehives and their colonies"),
		swaggerkit.WithTag("honey", "Honey harvests"),
		swaggerkit.WithTag("apiary", "The apiary as a whole"),
	)
	a := &Apiary{store: store, photos: photos{byHive: map[int64]photo{}}}
	hives := api.Group("/hives", swaggerkit.Tags("hives"))
	swaggerkit.Get(hives, "", a.ListHives)
	swaggerkit.Get(hives, "/{hiveId}", a.GetHive, swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Get(hives, "/{hiveId}/label", a.HiveLabel,
		swaggerkit.Summary("Print a hive label"),
		swaggerkit.Produces("text/plain"),
		swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Get(hives, "/{hiveId}/photo", a.GetPhoto,
		swaggerkit.Summary("Get the photo of a hive"),
		swaggerkit.Produces("image/*"),
		swaggerkit.Errors(http.StatusNotFound))

	// Changes need the beekeeper token.
	keeper := hives.Group("", swaggerkit.Security("beekeeper"), swaggerkit.Middlewares(requireBeekeeper(token)))
	swaggerkit.Post(keeper, "", a.CreateHive, swaggerkit.Status(http.StatusCreated))
	swaggerkit.Patch(keeper, "/{hiveId}", a.UpdateHive, swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Delete(keeper, "/{hiveId}", a.DeleteHive, swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Put(keeper, "/{hiveId}/photo", a.UploadPhoto,
		swaggerkit.Summary("Upload a photo of a hive"),
		swaggerkit.MaxBodyBytes(2<<20),
		swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Post(keeper, "/{hiveId}/bees", a.AddBees,
		swaggerkit.Summary("Move bees into a hive"),
		swaggerkit.Status(http.StatusOK),
		swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Post(keeper, "/{hiveId}/harvests", a.HarvestHoney,
		swaggerkit.Tags("honey"),
		swaggerkit.Status(http.StatusCreated),
		swaggerkit.Errors(http.StatusNotFound, http.StatusConflict))

	swaggerkit.Get(api, "/stats", a.GetStats, swaggerkit.Tags("apiary"), swaggerkit.Summary("Count hives, bees and honey"))
	return api
}
