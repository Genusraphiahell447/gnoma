package security

import (
	"context"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// SafeProvider wraps a provider.Provider with firewall redaction.
//
// On every Stream call it scans outgoing messages and the system prompt
// through the firewall referenced by fwRef before delegating to the
// inner provider. Name, Models, and DefaultModel pass through unchanged.
//
// Tool-result redaction stays in the engine because it needs per-tool
// context (origin tool name for logging) that isn't available at the
// provider boundary.
//
// SafeProvider is the single non-bypassable boundary between gnoma's
// internals and any LLM endpoint. Every provider.Provider registered
// with the router or handed to a non-engine consumer (SLM classifier,
// summarizer, hook streamer) should be wrapped.
type SafeProvider struct {
	inner provider.Provider
	fwRef *FirewallRef
}

// WrapProvider returns a SafeProvider that delegates to inner and
// resolves its firewall through ref. A nil ref is permitted and
// disables scanning — callers who never install a firewall get
// pass-through behaviour rather than a panic.
func WrapProvider(inner provider.Provider, ref *FirewallRef) *SafeProvider {
	return &SafeProvider{inner: inner, fwRef: ref}
}

// Inner returns the wrapped provider. Useful for tests and for code
// that needs to reach through to provider-specific behaviour.
func (p *SafeProvider) Inner() provider.Provider {
	return p.inner
}

// Marker is a sealed-trait interface only types defined in this package
// can satisfy. Higher layers (e.g. router.SecureProvider) embed Marker so
// the compiler — not convention — enforces that any provider flowing
// through them has been wrapped here. Adding `func (x *Foo) secured() {}`
// to a non-wrapped type in another package would not satisfy this
// interface, because Go method sets distinguish unexported methods by
// their defining package.
type Marker interface {
	secured()
}

// secured is the marker method that seals SecureProvider's embedded
// Marker interface. Intentionally no-op.
func (p *SafeProvider) secured() {}

func (p *SafeProvider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	if p.fwRef != nil {
		if fw := p.fwRef.Get(); fw != nil {
			req.Messages = fw.ScanOutgoingMessages(req.Messages)
			req.SystemPrompt = fw.ScanSystemPrompt(req.SystemPrompt)
		}
	}
	return p.inner.Stream(ctx, req)
}

func (p *SafeProvider) Name() string {
	return p.inner.Name()
}

func (p *SafeProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return p.inner.Models(ctx)
}

func (p *SafeProvider) DefaultModel() string {
	return p.inner.DefaultModel()
}

// Compile-time assertion that *SafeProvider satisfies provider.Provider.
var _ provider.Provider = (*SafeProvider)(nil)
