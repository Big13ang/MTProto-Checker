package server

import (
	"encoding/json"
	"net/http"
)

// ApiSuccessResponse wraps successful API responses.
type ApiSuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
}

// ApiErrorInfo contains error details.
type ApiErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ApiErrorResponse wraps error API responses.
type ApiErrorResponse struct {
	Success bool         `json:"success"`
	Error   ApiErrorInfo `json:"error"`
}

func statusCodeToErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusMethodNotAllowed:
		return "METHOD_NOT_ALLOWED"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusRequestEntityTooLarge:
		return "PAYLOAD_TOO_LARGE"
	case http.StatusUnsupportedMediaType:
		return "UNSUPPORTED_MEDIA_TYPE"
	case http.StatusTooManyRequests:
		return "RATE_LIMIT_EXCEEDED"
	case http.StatusInternalServerError:
		return "INTERNAL_SERVER_ERROR"
	default:
		return "INTERNAL_SERVER_ERROR"
	}
}

func jsonResponse(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	switch val := v.(type) {
	case ApiSuccessResponse, ApiErrorResponse:
		_ = json.NewEncoder(w).Encode(val)
	default:
		if status >= 400 {
			msg := "error"
			if m, ok := v.(map[string]string); ok && m["error"] != "" {
				msg = m["error"]
			} else if cr, ok := v.(CheckResponse); ok && !cr.OK {
				msg = "check failed"
			}
			_ = json.NewEncoder(w).Encode(ApiErrorResponse{
				Success: false,
				Error: ApiErrorInfo{
					Code:    statusCodeToErrorCode(status),
					Message: msg,
				},
			})
		} else {
			_ = json.NewEncoder(w).Encode(ApiSuccessResponse{
				Success: true,
				Data:    v,
			})
		}
	}
}
