package main

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/pluhsoft/swaggerkit"
)

// Apiary holds the handlers.
type Apiary struct {
	store *Store
}

type beekeeperKey struct{}

// BeekeeperFrom returns the authenticated beekeeper.
func BeekeeperFrom(ctx context.Context) string {
	name, _ := ctx.Value(beekeeperKey{}).(string)
	return name
}

// NewAPI wires the routes. token is the bearer token of the beekeeper.
func NewAPI(store *Store, token string, logger *slog.Logger) *swaggerkit.API {
	bearer := swaggerkit.BearerAuth(func(ctx context.Context, got string) (context.Context, error) {
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, swaggerkit.Unauthorized("unknown token")
		}
		return context.WithValue(ctx, beekeeperKey{}, "beekeeper"), nil
	})
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
	api.Use(
		swaggerkit.RequestLogger(logger),
		swaggerkit.CORS(swaggerkit.CORSOptions{AllowedOrigins: []string{"http://localhost:3000"}}),
	)

	a := &Apiary{store: store}
	hives := api.Group("/hives", swaggerkit.Tags("hives"))
	swaggerkit.Get(hives, "", a.ListHives)
	swaggerkit.Get(hives, "/{hiveId}", a.GetHive, swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Get(hives, "/{hiveId}/label", a.HiveLabel,
		swaggerkit.Summary("Print a hive label"),
		swaggerkit.Produces("text/plain"),
		swaggerkit.Errors(http.StatusNotFound))

	// Changes need the beekeeper token.
	keeper := hives.Group("", swaggerkit.Security("beekeeper"))
	swaggerkit.Post(keeper, "", a.CreateHive, swaggerkit.Status(http.StatusCreated))
	swaggerkit.Patch(keeper, "/{hiveId}", a.UpdateHive, swaggerkit.Errors(http.StatusNotFound))
	swaggerkit.Delete(keeper, "/{hiveId}", a.DeleteHive, swaggerkit.Errors(http.StatusNotFound))
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
