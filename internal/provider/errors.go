package provider

import (
	"fmt"
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

// ClassifyHTTPStatus returns the ErrorKind and retryability for an HTTP status code.
func ClassifyHTTPStatus(status int) (ErrorKind, bool) {
	switch {
	case status == 401 || status == 403:
		return ErrAuth, false
	case status == 400:
		return ErrBadRequest, false
	case status == 404:
		return ErrNotFound, false
	case status == 429 || status == 529:
		return ErrTransient, true
	case status == 500 || status == 502 || status == 503:
		return ErrTransient, true
	case status == 504:
		return ErrOverloaded, true
	default:
		if status >= 500 {
			return ErrTransient, true
		}
		return ErrBadRequest, false
	}
}
