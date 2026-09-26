package swaggerkit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type benchLocation struct {
	Lat float64 `json:"lat" validate:"min=-90,max=90"`
	Lon float64 `json:"lon" validate:"min=-180,max=180"`
}

type benchHive struct {
	Name     string        `json:"name" validate:"min=1,max=64"`
	Status   color         `json:"status,omitempty"`
	Bees     int           `json:"bees" validate:"min=0,max=100000"`
	Location benchLocation `json:"location"`
	Tags     []string      `json:"tags,omitempty" validate:"max=10,dive,max=32"`
}

type benchInput struct {
	ID    int64    `path:"id" validate:"min=1"`
	Limit int      `query:"limit" validate:"min=1,max=100" default:"20"`
	Tags  []string `query:"tag"`
	Body  benchHive
}

const benchBody = `{"name":"Linden","status":"red","bees":42000,"location":{"lat":55.75,"lon":37.62},"tags":["meadow","calm"]}`

func benchLargeBody(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = benchBody
	}
	return "[" + strings.Join(items, ",") + "]"
}

func benchAPI() *API {
	api := New(Info{Title: "Bench"}, WithLogger(slog.New(slog.DiscardHandler)), WithMaxBodyBytes(10<<20))
	Post(api, "/hives/{id}", func(ctx context.Context, in benchInput) (benchHive, error) { return in.Body, nil })
	Post(api, "/bulk", func(ctx context.Context, in []benchHive) (int, error) { return len(in), nil })
	Get(api, "/hives/{id}", func(ctx context.Context, in struct {
		ID    int64    `path:"id" validate:"min=1"`
		Limit int      `query:"limit" validate:"min=1,max=100" default:"20"`
		Tags  []string `query:"tag"`
	}) (int64, error) {
		return in.ID, nil
	})
	return api
}

func benchServe(b *testing.B, api *API, method, target, body string, want int) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != want {
			b.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
}

// BenchmarkRequest measures a full request: routing, parameters, body, handler, response.
func BenchmarkRequest(b *testing.B) {
	benchServe(b, benchAPI(), http.MethodPost, "/hives/7?limit=10&tag=a&tag=b", benchBody, 200)
}

// BenchmarkParams measures a request without a body.
func BenchmarkParams(b *testing.B) {
	benchServe(b, benchAPI(), http.MethodGet, "/hives/7?limit=10&tag=a&tag=b", "", 200)
}

// BenchmarkLargeBody measures a body with 1000 objects.
func BenchmarkLargeBody(b *testing.B) {
	benchServe(b, benchAPI(), http.MethodPost, "/bulk", benchLargeBody(1000), 200)
}

// BenchmarkBaselineUnmarshal is plain encoding/json on the same body, for comparison.
func BenchmarkBaselineUnmarshal(b *testing.B) {
	for _, n := range []int{1, 1000} {
		body := benchBody
		if n > 1 {
			body = benchLargeBody(n)
		}
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for b.Loop() {
				data, _ := io.ReadAll(strings.NewReader(body))
				var v any
				if n == 1 {
					v = new(benchHive)
				} else {
					v = new([]benchHive)
				}
				if err := json.Unmarshal(data, v); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
