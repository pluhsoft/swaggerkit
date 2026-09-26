package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pluhsoft/swaggerkit"
)

var update = flag.Bool("update", false, "rewrite openapi.json")

func newTestAPI() *swaggerkit.API {
	return NewAPI(NewStore(), "beekeeper", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestOpenAPI keeps the committed documents in sync with the code:
//
//	go test ./examples/apiary -run TestOpenAPI -update
func TestOpenAPI(t *testing.T) {
	api := newTestAPI()
	for file, format := range map[string]swaggerkit.Format{
		"openapi.json":     swaggerkit.FormatOpenAPI31,
		"openapi-3.0.json": swaggerkit.FormatOpenAPI30,
		"swagger.json":     swaggerkit.FormatSwagger20,
	} {
		doc, err := api.Document(format)
		if err != nil {
			t.Fatal(err)
		}
		if *update {
			if err := os.WriteFile(file, doc, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(doc, want) {
			t.Errorf("%s is outdated; run: go test ./examples/apiary -run TestOpenAPI -update", file)
		}
	}
}

func TestLint(t *testing.T) {
	for _, issue := range newTestAPI().Lint() {
		t.Errorf("unexpected lint issue: %s", issue)
	}
}

type call struct {
	method, path, body, token string
	wantStatus                int
	wantBody                  string // substring
}

func TestPhotoUpload(t *testing.T) {
	api := newTestAPI()
	upload := func(content, token string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("caption", "Spring inspection")
		fw, _ := mw.CreateFormFile("photo", "linden.png")
		_, _ = fw.Write([]byte(content))
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/hives/1/photo", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32)

	if rec := upload(png, "wrong"); rec.Code != 401 {
		t.Fatalf("without token: %d", rec.Code)
	}
	if rec := upload("not an image", "beekeeper"); rec.Code != 422 || !strings.Contains(rec.Body.String(), "must be a JPEG or PNG image") {
		t.Fatalf("text file: %d %s", rec.Code, rec.Body)
	}
	if rec := upload(png, "beekeeper"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"contentType":"image/png"`) {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/hives/1/photo", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || rec.Body.String() != png {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}
}

func TestAPI(t *testing.T) {
	api := newTestAPI()
	calls := []call{
		{method: "GET", path: "/api/v1/hives?limit=2", wantStatus: 200, wantBody: `"total":3`},
		{method: "GET", path: "/api/v1/hives?status=swarming", wantStatus: 200, wantBody: `"name":"Clover"`},
		{method: "GET", path: "/api/v1/hives?status=sleeping", wantStatus: 422, wantBody: `"location":"query.status"`},
		{method: "GET", path: "/api/v1/hives?limit=1000", wantStatus: 422, wantBody: `must be less than or equal to 100`},
		{method: "GET", path: "/api/v1/hives/1", wantStatus: 200, wantBody: `"name":"Linden"`},
		{method: "GET", path: "/api/v1/hives/abc", wantStatus: 422, wantBody: `"location":"path.hiveId"`},
		{method: "GET", path: "/api/v1/hives/99", wantStatus: 404, wantBody: `hive 99 does not exist`},
		{method: "POST", path: "/api/v1/hives", body: `{"name":"Acacia","location":{"lat":55,"lon":37}}`, wantStatus: 401},
		{method: "POST", path: "/api/v1/hives", body: `{"name":"Acacia","location":{"lat":55,"lon":37}}`, token: "wrong", wantStatus: 401, wantBody: `unknown token`},
		{method: "POST", path: "/api/v1/hives", body: `{"name":"Acacia","location":{"lat":55,"lon":37}}`, token: "beekeeper", wantStatus: 201, wantBody: `"status":"empty"`},
		{method: "POST", path: "/api/v1/hives", body: `{"name":"","location":{"lat":95,"lon":37},"color":"red"}`, token: "beekeeper", wantStatus: 422, wantBody: `"body.location.lat"`},
		{method: "POST", path: "/api/v1/hives", body: `{"name":`, token: "beekeeper", wantStatus: 400},
		{method: "POST", path: "/api/v1/hives/4/bees", body: `{"count":5000}`, token: "beekeeper", wantStatus: 200, wantBody: `"status":"active"`},
		{method: "POST", path: "/api/v1/hives/1/harvests", body: `{"kg":4.5}`, token: "beekeeper", wantStatus: 201, wantBody: `"remainingKg":8`},
		{method: "POST", path: "/api/v1/hives/1/harvests", body: `{"kg":500}`, token: "beekeeper", wantStatus: 422},
		{method: "POST", path: "/api/v1/hives/1/harvests", body: `{"kg":10}`, token: "beekeeper", wantStatus: 409, wantBody: `only 6.0 kg`},
		{method: "PATCH", path: "/api/v1/hives/2", body: `{"status":"dormant"}`, token: "beekeeper", wantStatus: 200, wantBody: `"status":"dormant"`},
		{method: "GET", path: "/api/v1/hives/1/label", wantStatus: 200, wantBody: "Hive #1 Linden"},
		{method: "DELETE", path: "/api/v1/hives/3", token: "beekeeper", wantStatus: 204},
		{method: "GET", path: "/api/v1/stats", wantStatus: 200, wantBody: `"hives":3`},
		{method: "GET", path: "/api/v1/docs", wantStatus: 200, wantBody: "swagger-ui"},
		{method: "GET", path: "/api/v1/docs/openapi.json", wantStatus: 200, wantBody: `"openapi": "3.1.0"`},
	}
	for _, c := range calls {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			var body io.Reader
			if c.body != "" {
				body = strings.NewReader(c.body)
			}
			req := httptest.NewRequest(c.method, c.path, body)
			if c.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			api.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Fatalf("status %d, want %d; body: %s", rec.Code, c.wantStatus, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantBody) {
				t.Fatalf("body %s does not contain %s", rec.Body, c.wantBody)
			}
			if rec.Code >= 400 {
				var p map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p["status"] != float64(rec.Code) {
					t.Fatalf("error body is not problem details: %s", rec.Body)
				}
			}
		})
	}
}
