package anthropic

import (
	"errors"
	"fmt"
)

// Sentinel errors for client construction, request execution, and HTTP response translation.
var (
	// ErrAPIKeyRequired is New's error when Options.APIKey is empty.
	ErrAPIKeyRequired = errors.New("anthropic: api key is required")
	// ErrInvalidOptions is New's error when an option field is negative or invalid.
	ErrInvalidOptions = errors.New("anthropic: invalid options")
	// ErrAuth reports 401 Unauthorized or 403 Forbidden responses.
	ErrAuth = errors.New("anthropic: authentication failed")
	// ErrRateLimited reports 429 Too Many Requests responses.
	ErrRateLimited = errors.New("anthropic: rate limited")
	// ErrBadRequest reports 400 Bad Request responses.
	ErrBadRequest = errors.New("anthropic: bad request")
	// ErrServer reports 5xx server responses.
	ErrServer = errors.New("anthropic: server error")
	// ErrRefused reports turn termination caused by model refusal.
	ErrRefused = errors.New("anthropic: model refused request")
)

// mapAPIErrorType classifies an Anthropic API error by its "type"
// field, the same classification mapHTTPError applies by status code.
// A mid-stream "error" event carries no HTTP status, so this is the
// only classifier available on that path; handleErrorEvent is its one
// caller. An unrecognized or empty type maps to ErrServer, since every
// documented mid-stream error (overloaded_error, api_error) is a
// transient server condition, not a caller mistake.
func mapAPIErrorType(errType, message string) error {
	if message == "" {
		message = errType
	}
	if message == "" {
		message = "stream error"
	}
	switch errType {
	case "authentication_error", "permission_error":
		return fmt.Errorf("%w: %s", ErrAuth, message)
	case "rate_limit_error":
		return fmt.Errorf("%w: %s", ErrRateLimited, message)
	case "invalid_request_error", "not_found_error":
		return fmt.Errorf("%w: %s", ErrBadRequest, message)
	default:
		return fmt.Errorf("%w: %s", ErrServer, message)
	}
}
