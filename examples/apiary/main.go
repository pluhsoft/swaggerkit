// Command apiary is an example API built with swaggerkit.
//
//	go run ./examples/apiary                          # serve on :8080, docs at /api/v1/docs
//	go run ./examples/apiary -openapi openapi.json    # write the OpenAPI document and exit
//	go run ./examples/apiary -lint                    # print API design hints and exit
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/pluhsoft/swaggerkit"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	specFile := flag.String("openapi", "", "write the OpenAPI document to `file` and exit")
	lint := flag.Bool("lint", false, "print API design hints and exit")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	token := os.Getenv("APIARY_TOKEN")
	if token == "" {
		token = "beekeeper"
	}
	api := NewAPI(NewStore(), token, logger)

	switch {
	case *specFile != "":
		if err := writeSpec(api, *specFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case *lint:
		failed := false
		for _, issue := range api.Lint() {
			fmt.Println(issue)
			failed = failed || issue.Severity == swaggerkit.SeverityError
		}
		if failed {
			os.Exit(1)
		}
	default:
		srv := &http.Server{
			Addr:              *addr,
			Handler:           api,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       time.Minute,
		}
		logger.Info("apiary is open", "docs", "http://localhost"+*addr+"/api/v1/docs")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}
}

func writeSpec(api *swaggerkit.API, path string) error {
	doc, err := api.OpenAPI()
	if err != nil {
		return err
	}
	return os.WriteFile(path, doc, 0o644)
}
