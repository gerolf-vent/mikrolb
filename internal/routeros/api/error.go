package api

import (
	"errors"
	"fmt"
)

type Error struct {
	Code    uint   `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

func (e Error) Error() string {
	return fmt.Sprintf("API error: code %d, message: %s, detail: %s", e.Code, e.Message, e.Detail)
}

func IsUnauthorized(err error) bool {
	var apiErr Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 401
	}
	return false
}

func IsForbidden(err error) bool {
	var apiErr Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 403
	}
	return false
}

func IsNotFound(err error) bool {
	var apiErr Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 404
	}
	return false
}
