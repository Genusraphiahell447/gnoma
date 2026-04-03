package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestProviderError_Error(t *testing.T) {
	err := &ProviderError{
		Kind:       ErrTransient,
		Provider:   "mistral",
		StatusCode: 429,
		Message:    "rate limited",
	}
	got := err.Error()
	want := "mistral transient (429): rate limited"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestProviderError_Error_WithWrapped(t *testing.T) {
	inner := errors.New("connection reset")
	err := &ProviderError{
		Kind:       ErrTransient,
		Provider:   "openai",
		StatusCode: 502,
		Message:    "bad gateway",
		Err:        inner,
	}
	got := err.Error()
	want := "openai transient (502): bad gateway: connection reset"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	inner := errors.New("timeout")
	err := &ProviderError{
		Kind: ErrTransient,
		Err:  inner,
	}
	if !errors.Is(err, inner) {
		t.Error("errors.Is should find inner error")
	}
}

func TestProviderError_AsType(t *testing.T) {
	inner := &ProviderError{
		Kind:       ErrAuth,
		Provider:   "anthropic",
		StatusCode: 401,
		Message:    "invalid key",
	}
	wrapped := fmt.Errorf("api call failed: %w", inner)

	pErr, ok := errors.AsType[*ProviderError](wrapped)
	if !ok {
		t.Fatal("errors.AsType should find ProviderError")
	}
	if pErr.Kind != ErrAuth {
		t.Errorf("Kind = %v, want %v", pErr.Kind, ErrAuth)
	}
	if pErr.Provider != "anthropic" {
		t.Errorf("Provider = %q", pErr.Provider)
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		status    int
		wantKind  ErrorKind
		wantRetry bool
	}{
		{200, ErrBadRequest, false}, // shouldn't happen, but safe default
		{400, ErrBadRequest, false},
		{401, ErrAuth, false},
		{403, ErrAuth, false},
		{404, ErrNotFound, false},
		{429, ErrTransient, true},
		{500, ErrTransient, true},
		{502, ErrTransient, true},
		{503, ErrTransient, true},
		{504, ErrOverloaded, true},
		{529, ErrTransient, true},
		{599, ErrTransient, true}, // unknown 5xx
	}
	for _, tt := range tests {
		kind, retry := ClassifyHTTPStatus(tt.status)
		if kind != tt.wantKind {
			t.Errorf("ClassifyHTTPStatus(%d) kind = %v, want %v", tt.status, kind, tt.wantKind)
		}
		if retry != tt.wantRetry {
			t.Errorf("ClassifyHTTPStatus(%d) retry = %v, want %v", tt.status, retry, tt.wantRetry)
		}
	}
}

func TestErrorKind_String(t *testing.T) {
	tests := []struct {
		kind ErrorKind
		want string
	}{
		{ErrTransient, "transient"},
		{ErrAuth, "auth"},
		{ErrBadRequest, "bad_request"},
		{ErrNotFound, "not_found"},
		{ErrOverloaded, "overloaded"},
		{ErrorKind(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("ErrorKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
