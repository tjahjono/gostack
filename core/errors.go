// Package core defines persistence-independent framework values and errors.
package core

import "errors"

type Code string

const (
	Invalid      Code = "invalid"
	NotFound     Code = "not_found"
	Conflict     Code = "conflict"
	Unauthorized Code = "unauthorized"
	Forbidden    Code = "forbidden"
	Unavailable  Code = "unavailable"
)

// Error exposes a safe message while retaining an internal diagnostic cause.
type Error struct {
	code    Code
	message string
	cause   error
}

func NewError(code Code, message string, cause error) *Error {
	return &Error{code: code, message: message, cause: cause}
}
func (e *Error) Error() string { return e.message }
func (e *Error) Code() Code    { return e.code }
func (e *Error) Unwrap() error { return e.cause }
func IsCode(err error, code Code) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Code() == code
}

// PageRequest bounds list work. Cursors are verified by the owning module.
type PageRequest struct {
	limit  int
	cursor string
}

func NewPageRequest(limit int, cursor string) (PageRequest, error) {
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 || len(cursor) > 4096 {
		return PageRequest{}, NewError(Invalid, "invalid pagination", nil)
	}
	return PageRequest{limit: limit, cursor: cursor}, nil
}
func (p PageRequest) Limit() int {
	if p.limit == 0 {
		return 25
	}
	return p.limit
}
func (p PageRequest) Cursor() string { return p.cursor }

type Page[T any] struct {
	Items      []T
	NextCursor string
}
