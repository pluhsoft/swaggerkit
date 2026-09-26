package main

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"time"

	"github.com/pluhsoft/swaggerkit"
)

type photo struct {
	data        []byte
	contentType string
	name        string
	caption     string
	uploadedAt  time.Time
}

// photos keeps one photo per hive.
type photos struct {
	mu     sync.Mutex
	byHive map[int64]photo
}

// PhotoInfo describes an uploaded photo.
type PhotoInfo struct {
	HiveID      int64  `json:"hiveId" doc:"Hive ID" example:"1"`
	Name        string `json:"name" doc:"Original file name" example:"linden.jpg"`
	ContentType string `json:"contentType" doc:"image/jpeg or image/png" example:"image/jpeg"`
	Size        int    `json:"size" doc:"Size in bytes" example:"48213"`
	Caption     string `json:"caption,omitempty" doc:"Caption" example:"Spring inspection"`
}

// Doc describes the type in the OpenAPI document.
func (PhotoInfo) Doc() string { return "A photo of a hive." }

// UploadPhotoInput is a multipart form.
type UploadPhotoInput struct {
	HivePath
	Photo   *multipart.FileHeader `form:"photo" validate:"required" doc:"JPEG or PNG image, up to 2 MiB"`
	Caption string                `form:"caption" validate:"optional,max=100" doc:"Caption"`
}

// UploadPhoto replaces the photo of a hive.
func (a *Apiary) UploadPhoto(ctx context.Context, in UploadPhotoInput) (PhotoInfo, error) {
	if _, ok := a.store.Get(in.ID); !ok {
		return PhotoInfo{}, errHiveNotFound(in.ID)
	}
	f, err := in.Photo.Open()
	if err != nil {
		return PhotoInfo{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return PhotoInfo{}, err
	}
	// Trust the bytes, not the client's Content-Type.
	ct := http.DetectContentType(data)
	if ct != "image/jpeg" && ct != "image/png" {
		return PhotoInfo{}, &swaggerkit.Error{Status: http.StatusUnprocessableEntity,
			Errors: []swaggerkit.FieldError{{Location: "form.photo", Message: "must be a JPEG or PNG image"}}}
	}
	a.photos.mu.Lock()
	a.photos.byHive[in.ID] = photo{data: data, contentType: ct, name: in.Photo.Filename, caption: in.Caption, uploadedAt: time.Now().UTC()}
	a.photos.mu.Unlock()
	return PhotoInfo{HiveID: in.ID, Name: in.Photo.Filename, ContentType: ct, Size: len(data), Caption: in.Caption}, nil
}

// GetPhoto returns the photo of a hive.
func (a *Apiary) GetPhoto(ctx context.Context, in HivePath) (*swaggerkit.File, error) {
	a.photos.mu.Lock()
	p, ok := a.photos.byHive[in.ID]
	a.photos.mu.Unlock()
	if !ok {
		return nil, swaggerkit.NotFound("the hive has no photo")
	}
	return &swaggerkit.File{Body: bytes.NewReader(p.data), ContentType: p.contentType, Name: p.name, Inline: true, ModTime: p.uploadedAt}, nil
}
