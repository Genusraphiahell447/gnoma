package provider

import (
	"fmt"
	"strings"
	"time"
)

// ErrorKind classifies provider errors for retry decisions.
type ErrorKind int

const (
	ErrTransient  ErrorKind = iota + 1 // 429, 500, 502, 503, 529 — retry with backoff
	ErrAuth                            // 401, 403 — don't retry
	ErrBadRequest                      // 400 — don't retry, fix request
	ErrNotFound                        // 404 — model/endpoint not found
	ErrOverloaded                      // capacity exhausted — backoff + retry
)

func (k ErrorKind) String() string {
	switch k {
	case ErrTransient:
		return "transient"
	case ErrAuth:
		return "auth"
	case ErrBadRequest:
		return "bad_request"
	case ErrNotFound:
		return "not_found"
	case ErrOverloaded:
		return "overloaded"
	default:
		return fmt.Sprintf("unknown(%d)", k)
	}
}

// ProviderError wraps an SDK error with classification metadata.
type ProviderError struct {
	Kind       ErrorKind
	Provider   string
	StatusCode int
	Message    string
	Retryable  bool
	RetryAfter time.Duration // from Retry-After or rate limit headers
	Err        error         // underlying SDK error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s %s (%d): %s: %v", e.Provider, e.Kind, e.StatusCode, e.Message, e.Err)
	}
	return fmt.Sprintf("%s %s (%d): %s", e.Provider, e.Kind, e.StatusCode, e.Message)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// nonRetryable500Substrings lists error messages from servers (e.g. llama.cpp)
// that return 500 for deterministic client-side failures. These should not be
// retried because the same request will always produce the same error.
var nonRetryable500Substrings = []string{
	"Failed to parse tool call",   // llama.cpp: model output invalid tool call JSON
	"failed to parse tool call",   // lowercase variant
	"tool_call_error",             // some servers use this error type
	"invalid_tool_call",           // OpenAI-compat servers
}

// ClassifyHTTPError classifies an HTTP error using both status code and the
// error message. This catches deterministic 500s (e.g. llama.cpp tool parse
// failures) that should not be retried.
func ClassifyHTTPError(status int, message string) (ErrorKind, bool) {
	if status == 500 && message != "" {
		lower := strings.ToLower(message)
		for _, substr := range nonRetryable500Substrings {
			if strings.Contains(lower, strings.ToLower(substr)) {
				return ErrBadRequest, false
			}
		}
	}
	return ClassifyHTTPStatus(status)
}

// ClassifyHTTPStatus returns the ErrorKind and retryability for an HTTP status code.
func ClassifyHTTPStatus(status int) (ErrorKind, bool) {
	switch status {
	case 401, 403:
		return ErrAuth, false
	case 400:
		return ErrBadRequest, false
	case 404:
		return ErrNotFound, false
	case 429, 529:
		return ErrTransient, true
	case 500, 502, 503:
		return ErrTransient, true
	case 504:
		return ErrOverloaded, true
	default:
		if status >= 500 {
			return ErrTransient, true
		}
		return ErrBadRequest, false
	}
}
