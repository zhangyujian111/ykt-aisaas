// Package scim provides SCIM v2.0 error types.
package scim

import (
	"fmt"
	"net/http"
)

// SCIM Protocol Errors (RFC 7644)
const (
	// ScimErrorInvalidFilter indicates an invalid filter syntax.
	ScimErrorInvalidFilter = "invalidFilter"
	// ScimErrorTooMany indicates too many operations in a request.
	ScimErrorTooMany = "tooMany"
	// ScimErrorUniqueness indicates an attribute value is not unique.
	ScimErrorUniqueness = "uniqueness"
	// ScimErrorMutability indicates an attribute is immutable.
	ScimErrorMutability = "mutability"
	// ScimErrorNoTarget indicates the target resource does not exist.
	ScimErrorNoTarget = "noTarget"
	// ScimErrorInvalidValue indicates an invalid value.
	ScimErrorInvalidValue = "invalidValue"
	// ScimErrorInvalidSyntax indicates an invalid request syntax.
	ScimErrorInvalidSyntax = "invalidSyntax"
	// ScimErrorPartialFailure indicates a partial failure.
	ScimErrorPartialFailure = "partialFailure"
	// ScimErrorConflict indicates a conflict with existing resource.
	ScimErrorConflict = "conflict"
)

// ScimException represents a SCIM protocol error.
type ScimException struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func (e *ScimException) Error() string {
	return fmt.Sprintf("%s: %s (status %d)", e.Code, e.Message, e.Status)
}

// NewBadRequestException creates a 400 Bad Request error.
func NewBadRequestException(code, message string) *ScimException {
	return &ScimException{
		Code:    code,
		Message: message,
		Status:  http.StatusBadRequest,
	}
}

// NewUnauthorizedException creates a 401 Unauthorized error.
func NewUnauthorizedException(message string) *ScimException {
	return &ScimException{
		Code:    "unauthorized",
		Message: message,
		Status:  http.StatusUnauthorized,
	}
}

// NewForbiddenException creates a 403 Forbidden error.
func NewForbiddenException(message string) *ScimException {
	return &ScimException{
		Code:    "forbidden",
		Message: message,
		Status:  http.StatusForbidden,
	}
}

// NewNotFoundException creates a 404 Not Found error.
func NewNotFoundException(message string) *ScimException {
	return &ScimException{
		Code:    "notFound",
		Message: message,
		Status:  http.StatusNotFound,
	}
}

// NewConflictException creates a 409 Conflict error.
func NewConflictException(message string) *ScimException {
	return &ScimException{
		Code:    ScimErrorConflict,
		Message: message,
		Status:  http.StatusConflict,
	}
}

// NewInternalServerErrorException creates a 500 Internal Server Error.
func NewInternalServerErrorException(message string) *ScimException {
	return &ScimException{
		Code:    "internalServerError",
		Message: message,
		Status:  http.StatusInternalServerError,
	}
}

// ErrorResponse returns a SCIM error response in the format specified by RFC 7644.
type ErrorResponse struct {
	Schemas []string       `json:"schemas"`
	ScimException *ScimException `json:"scimType,omitempty"`
	Detail  string         `json:"detail"`
	Status  int            `json:"status"`
}

// NewErrorResponse creates a new error response.
func NewErrorResponse(err *ScimException) ErrorResponse {
	return ErrorResponse{
		Schemas:        []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
		ScimException: err,
		Detail:        err.Message,
		Status:        err.Status,
	}
}
