package logic

import (
	"encoding/json"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// apiError is the module's error envelope: a stable machine code, a human message, the
// offending field when there is one, and whether retrying the same request can succeed.
type apiError struct {
	Status    uint16 `json:"-"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Field     string `json:"field,omitempty"`
	Retryable bool   `json:"retryable"`
	Detail    any    `json:"detail,omitempty"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func (e *apiError) response() modulesdk.Response {
	return jsonResponse(e.Status, map[string]any{"error": e})
}

func errInvalid(field, msg string) *apiError {
	return &apiError{Status: 400, Code: "INVALID_ARGUMENT", Message: msg, Field: field}
}

func errUnauthenticated(msg string) *apiError {
	return &apiError{Status: 401, Code: "UNAUTHENTICATED", Message: msg}
}

func errNotFound(msg string) *apiError {
	return &apiError{Status: 404, Code: "NOT_FOUND", Message: msg}
}

func errConflict(code, msg string, retryable bool) *apiError {
	return &apiError{Status: 409, Code: code, Message: msg, Retryable: retryable}
}

func errInternal(msg string) *apiError {
	return &apiError{Status: 500, Code: "INTERNAL", Message: msg, Retryable: true}
}

func jsonResponse(status uint16, v any) modulesdk.Response {
	b, err := json.Marshal(v)
	if err != nil {
		return modulesdk.Response{Status: 500, Body: `{"error":{"code":"INTERNAL","message":"response encoding failed","retryable":false}}`}
	}
	return modulesdk.Response{Status: status, Headers: map[string]string{"content-type": "application/json"}, Body: string(b)}
}
