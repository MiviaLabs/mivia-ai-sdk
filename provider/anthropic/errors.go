package anthropic

import "errors"

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
