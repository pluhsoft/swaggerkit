package swaggerkit

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is an HTTP error sent to the client as RFC 9457 problem details
// (Content-Type: application/problem+json).
//
// Return it from a handler to choose the status code. Any other error is
// logged and answered with 500 Internal Server Error without details.
type Error struct {
	Type   string       `json:"type"`             // URI of the problem type; defaults to "about:blank"
	Title  string       `json:"title"`            // short summary; defaults to the status text
	Status int          `json:"status"`           // HTTP status code
	Detail string       `json:"detail,omitempty"` // explanation for the client
	Errors []FieldError `json:"errors,omitempty"` // invalid request values

	// Err is the cause. It is logged for 5xx errors and never sent to the client.
	Err error `json:"-"`
}

// NewError returns an error with the given status and detail message.
func NewError(status int, detail string) *Error {
	return &Error{Status: status, Detail: detail}
}

// Errorf returns an error with the given status and a formatted detail message.
// The %w verb sets Err; the wrapped error text still becomes part of Detail.
func Errorf(status int, format string, args ...any) *Error {
	err := fmt.Errorf(format, args...)
	return &Error{Status: status, Detail: err.Error(), Err: errors.Unwrap(err)}
}

// BadRequest returns a 400 error.
func BadRequest(detail string) *Error { return NewError(http.StatusBadRequest, detail) }

// Unauthorized returns a 401 error.
func Unauthorized(detail string) *Error { return NewError(http.StatusUnauthorized, detail) }

// Forbidden returns a 403 error.
func Forbidden(detail string) *Error { return NewError(http.StatusForbidden, detail) }

// NotFound returns a 404 error.
func NotFound(detail string) *Error { return NewError(http.StatusNotFound, detail) }

// Conflict returns a 409 error.
func Conflict(detail string) *Error { return NewError(http.StatusConflict, detail) }

// Error implements the error interface.
func (e *Error) Error() string {
	text := fmt.Sprintf("%d %s", e.Status, e.title())
	if e.Detail != "" {
		text += ": " + e.Detail
	}
	if e.Err != nil {
		text += ": " + e.Err.Error()
	}
	return text
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

func (e *Error) title() string {
	if e.Title != "" {
		return e.Title
	}
	if t := http.StatusText(e.Status); t != "" {
		return t
	}
	return "Error"
}

// problem returns a copy ready to be sent.
func (e *Error) problem() *Error {
	p := *e
	if p.Status < 400 || p.Status > 599 {
		p.Status = http.StatusInternalServerError
	}
	p.Title = p.title()
	if p.Type == "" {
		p.Type = "about:blank"
	}
	return &p
}

func validationError(status int, detail string, errs []FieldError) *Error {
	return &Error{Status: status, Detail: detail, Errors: errs}
}

// problemSchema documents the Error type in the OpenAPI document.
func problemSchema() *Schema {
	return &Schema{
		Type:        "object",
		Description: "Problem details, RFC 9457.",
		Properties: map[string]*Schema{
			"type":   {Type: "string", Format: "uri-reference", Examples: []any{"about:blank"}},
			"title":  {Type: "string", Examples: []any{"Unprocessable Entity"}},
			"status": {Type: "integer", Format: "int32", Examples: []any{422}},
			"detail": {Type: "string", Examples: []any{"request validation failed"}},
			"errors": {Type: "array", Items: &Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"location": {Type: "string", Examples: []any{"body.name"}},
					"message":  {Type: "string", Examples: []any{"is required"}},
				},
				PropertyOrder: []string{"location", "message"},
				Required:      []string{"location", "message"},
			}},
		},
		PropertyOrder: []string{"type", "title", "status", "detail", "errors"},
		Required:      []string{"type", "title", "status"},
	}
}
