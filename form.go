package swaggerkit

import (
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
)

const inForm = "form"

var fileHeaderType = reflect.TypeFor[*multipart.FileHeader]()

// formField is an input field tagged form:"name". Scalar fields reuse the
// parameter rules; file fields hold *multipart.FileHeader values.
type formField struct {
	param    *paramPlan // scalar value, nil for files
	name     string
	index    []int
	files    bool // *multipart.FileHeader or []*multipart.FileHeader
	multi    bool // []*multipart.FileHeader
	required bool
	maxFiles int // 0: no limit
	schema   *Schema
}

func newFormField(g *schemaGen, sf reflect.StructField, index []int, name, where string) *formField {
	if name == "" {
		fail(where, `form tag needs a name, e.g. form:"photo"`)
	}
	f := &formField{name: name, index: index}
	switch sf.Type {
	case fileHeaderType, reflect.SliceOf(fileHeaderType):
		f.files = true
		f.multi = sf.Type.Kind() == reflect.Slice
		file := &Schema{Type: "string", Format: "binary"}
		f.schema = file
		if f.multi {
			f.schema = &Schema{Type: "array", Items: file}
		}
		p, err := applyTags(f.schema, sf.Tag, g)
		if err != nil {
			fail(where, "%v", err)
		}
		f.required = p == presenceRequired
		if f.schema.MaxItems != nil {
			f.maxFiles = *f.schema.MaxItems
		}
	default:
		f.param = newParamPlan(g, sf, index, inForm, name, where)
		f.required = f.param.required
		f.schema = f.param.schema
	}
	return f
}

// hasFiles reports whether the form needs multipart/form-data.
func formHasFiles(fields []*formField) bool {
	for _, f := range fields {
		if f.files {
			return true
		}
	}
	return false
}

// bindForm parses a form body into the fields of in.
func (p *inputPlan) bindForm(a *API, w http.ResponseWriter, r *http.Request, in reflect.Value, limit int64) ([]FieldError, *Error) {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	files := formHasFiles(p.form)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	var err error
	switch {
	case mt == "multipart/form-data":
		err = r.ParseMultipartForm(min(limit, 32<<20)) // larger files go to temporary files
	case mt == "application/x-www-form-urlencoded" && !files:
		err = r.ParseForm()
	default:
		want := "multipart/form-data"
		if !files {
			want += " or application/x-www-form-urlencoded"
		}
		return nil, NewError(http.StatusUnsupportedMediaType, "Content-Type must be "+want)
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf("request body must not exceed %d bytes", limit))
		}
		return nil, BadRequest("request body is not a valid form")
	}

	values := r.PostForm
	var uploads map[string][]*multipart.FileHeader
	if r.MultipartForm != nil {
		values, uploads = r.MultipartForm.Value, r.MultipartForm.File
	}
	var errs []FieldError
	for _, f := range p.form {
		field := in.FieldByIndex(f.index)
		if f.param != nil {
			errs = append(errs, f.param.set(a, field, values[f.name])...)
			continue
		}
		list := uploads[f.name]
		loc := inForm + "." + f.name
		switch {
		case len(list) == 0 && f.required:
			errs = append(errs, FieldError{loc, "is required"})
		case len(list) == 0:
		case !f.multi && len(list) > 1:
			errs = append(errs, FieldError{loc, "must be a single file"})
		case f.maxFiles > 0 && len(list) > f.maxFiles:
			errs = append(errs, FieldError{loc, fmt.Sprintf("must contain at most %d files", f.maxFiles)})
		case f.multi:
			field.Set(reflect.ValueOf(list))
		default:
			field.Set(reflect.ValueOf(list[0]))
		}
	}
	return errs, nil
}

// formSchema documents the form as an object.
func formSchema(fields []*formField) *Schema {
	s := &Schema{Type: "object", Properties: map[string]*Schema{}}
	for _, f := range fields {
		s.Properties[f.name] = f.schema
		if f.required {
			s.addRequired(f.name)
		}
	}
	return s
}
