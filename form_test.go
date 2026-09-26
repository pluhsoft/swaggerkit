package swaggerkit

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type uploadInput struct {
	ID      int                     `path:"id"`
	Caption string                  `form:"caption" validate:"max=20" doc:"Caption"`
	Rating  int                     `form:"rating" validate:"optional,min=1,max=5"`
	Photo   *multipart.FileHeader   `form:"photo" validate:"required" doc:"JPEG or PNG"`
	Extra   []*multipart.FileHeader `form:"extra" validate:"max=2"`
}

type uploadResult struct {
	Caption string `json:"caption"`
	Rating  int    `json:"rating"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Extra   int    `json:"extra"`
}

func multipartBody(t *testing.T, fields map[string]string, files map[string][]string) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for name, contents := range files {
		for i, c := range contents {
			fw, err := mw.CreateFormFile(name, name+strings.Repeat("x", i)+".txt")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = fw.Write([]byte(c))
		}
	}
	_ = mw.Close()
	return mw.FormDataContentType(), &buf
}

func TestMultipartForm(t *testing.T) {
	api, _ := newTestAPI()
	Post(api, "/hives/{id}/photo", func(ctx context.Context, in uploadInput) (uploadResult, error) {
		f, err := in.Photo.Open()
		if err != nil {
			return uploadResult{}, err
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		return uploadResult{Caption: in.Caption, Rating: in.Rating, Name: in.Photo.Filename, Content: string(data), Extra: len(in.Extra)}, nil
	}, MaxBodyBytes(4096))

	send := func(ct string, body io.Reader) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/hives/1/photo", body)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}

	ct, body := multipartBody(t, map[string]string{"caption": "Linden", "rating": "4"}, map[string][]string{"photo": {"honey"}, "extra": {"a", "b"}})
	expect(t, send(ct, body), 200, `"caption":"Linden"`, `"rating":4`, `"name":"photo.txt"`, `"content":"honey"`, `"extra":2`)

	ct, body = multipartBody(t, map[string]string{"caption": strings.Repeat("x", 30), "rating": "9"}, map[string][]string{"extra": {"a", "b", "c"}})
	expect(t, send(ct, body), 422, `"form.caption"`, `"form.rating"`, `{"location":"form.photo","message":"is required"}`, `"must contain at most 2 files"`)

	ct, body = multipartBody(t, nil, map[string][]string{"photo": {"a", "b"}})
	expect(t, send(ct, body), 422, `"must be a single file"`)

	ct, body = multipartBody(t, nil, map[string][]string{"photo": {strings.Repeat("x", 5000)}})
	expect(t, send(ct, body), 413)

	expect(t, send("application/json", strings.NewReader(`{}`)), 415, "multipart/form-data")
	expect(t, send("multipart/form-data; boundary=x", strings.NewReader("garbage")), 400)

	doc, _ := api.OpenAPI()
	for _, want := range []string{`"multipart/form-data": {`, `"format": "binary"`, `"required": [`, `"description": "JPEG or PNG"`} {
		if !bytes.Contains(doc, []byte(want)) {
			t.Errorf("document does not contain %s", want)
		}
	}
	if bytes.Contains(doc, []byte("x-www-form-urlencoded")) {
		t.Error("forms with files accept multipart only")
	}
}

func TestURLEncodedForm(t *testing.T) {
	api, _ := newTestAPI()
	type in struct {
		Name string   `form:"name" validate:"min=1"`
		Tags []string `form:"tag"`
	}
	Post(api, "/hives", func(ctx context.Context, in in) (in, error) { return in, nil })
	form := url.Values{"name": {"Linden"}, "tag": {"a", "b"}}
	req := httptest.NewRequest(http.MethodPost, "/hives", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	expect(t, rec, 200, `"Name":"Linden"`, `"Tags":["a","b"]`)

	doc, _ := api.OpenAPI()
	if !bytes.Contains(doc, []byte(`"application/x-www-form-urlencoded": {`)) {
		t.Error("urlencoded form is not documented")
	}
}

func TestFormRegistrationErrors(t *testing.T) {
	type both struct {
		Name string `form:"name"`
		Body struct{}
	}
	type getForm struct {
		Name string `form:"name"`
	}
	type noName struct {
		Photo *multipart.FileHeader `form:""`
	}
	for _, tt := range []struct {
		reg  func(*API)
		want string
	}{
		{func(a *API) { Post(a, "/a", func(ctx context.Context, _ both) (string, error) { return "", nil }) }, "either a Body field or form fields"},
		{func(a *API) { Get(a, "/a", func(ctx context.Context, _ getForm) (string, error) { return "", nil }) }, "cannot have a form body"},
		{func(a *API) { Post(a, "/a", func(ctx context.Context, _ noName) (string, error) { return "", nil }) }, "form tag needs a name"},
	} {
		func() {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(panicText(r), tt.want) {
					t.Errorf("panic %v, want %q", r, tt.want)
				}
			}()
			api, _ := newTestAPI()
			tt.reg(api)
		}()
	}
}
