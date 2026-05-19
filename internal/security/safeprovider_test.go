package security

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// --- FirewallRef ---

func TestFirewallRef_GetBeforeSetReturnsNil(t *testing.T) {
	ref := new(FirewallRef)
	if fw := ref.Get(); fw != nil {
		t.Errorf("Get() before Set() = %v, want nil", fw)
	}
}

func TestFirewallRef_GetAfterSetReturnsValue(t *testing.T) {
	ref := new(FirewallRef)
	fw := NewFirewall(FirewallConfig{ScanOutgoing: true})
	ref.Set(fw)
	if got := ref.Get(); got != fw {
		t.Errorf("Get() = %p, want %p", got, fw)
	}
}

func TestFirewallRef_SetOverwritesPrevious(t *testing.T) {
	ref := new(FirewallRef)
	fw1 := NewFirewall(FirewallConfig{ScanOutgoing: true})
	fw2 := NewFirewall(FirewallConfig{ScanOutgoing: true})
	ref.Set(fw1)
	ref.Set(fw2)
	if got := ref.Get(); got != fw2 {
		t.Errorf("Get() = %p, want %p (second Set)", got, fw2)
	}
}

func TestFirewallRef_ConcurrentSetAndGetIsRaceSafe(t *testing.T) {
	ref := new(FirewallRef)
	fw := NewFirewall(FirewallConfig{ScanOutgoing: true})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ref.Set(fw)
		}()
		go func() {
			defer wg.Done()
			_ = ref.Get()
		}()
	}
	wg.Wait()

	if got := ref.Get(); got != fw {
		t.Errorf("after concurrent ops Get() = %p, want %p", got, fw)
	}
}

// --- recordingProvider ---

// recordingProvider captures the last Request it saw and lets tests
// assert what reached the provider boundary.
type recordingProvider struct {
	name      string
	lastReq   provider.Request
	streamErr error
}

func (p *recordingProvider) Name() string         { return p.name }
func (p *recordingProvider) DefaultModel() string { return "rec-model" }
func (p *recordingProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{
		ID:       "rec-model",
		Name:     "rec-model",
		Provider: p.name,
	}}, nil
}
func (p *recordingProvider) Stream(_ context.Context, req provider.Request) (stream.Stream, error) {
	p.lastReq = req
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	return &noopStream{}, nil
}

type noopStream struct{}

func (s *noopStream) Next() bool           { return false }
func (s *noopStream) Current() stream.Event { return stream.Event{} }
func (s *noopStream) Err() error            { return nil }
func (s *noopStream) Close() error          { return nil }

// --- SafeProvider ---

func TestSafeProvider_NilRefDelegatesWithoutScanning(t *testing.T) {
	rec := &recordingProvider{name: "rec"}
	sp := WrapProvider(rec, nil)

	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	req := provider.Request{
		SystemPrompt: "system contains " + secret,
		Messages: []message.Message{
			message.NewUserText("user contains " + secret),
		},
	}

	if _, err := sp.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream() err = %v", err)
	}

	if !strings.Contains(rec.lastReq.SystemPrompt, secret) {
		t.Errorf("nil ref scrubbed system prompt: %q", rec.lastReq.SystemPrompt)
	}
	if got := rec.lastReq.Messages[0].TextContent(); !strings.Contains(got, secret) {
		t.Errorf("nil ref scrubbed user message: %q", got)
	}
}

func TestSafeProvider_EmptyRefDelegatesWithoutScanning(t *testing.T) {
	// A *FirewallRef whose pointer is unset should behave like nil ref.
	rec := &recordingProvider{name: "rec"}
	sp := WrapProvider(rec, new(FirewallRef))

	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	req := provider.Request{
		Messages: []message.Message{message.NewUserText(secret)},
	}

	if _, err := sp.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream() err = %v", err)
	}

	if got := rec.lastReq.Messages[0].TextContent(); !strings.Contains(got, secret) {
		t.Errorf("empty ref scrubbed message: %q", got)
	}
}

func TestSafeProvider_RedactsOutgoingMessages(t *testing.T) {
	rec := &recordingProvider{name: "rec"}
	ref := new(FirewallRef)
	ref.Set(NewFirewall(FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 4.5,
	}))
	sp := WrapProvider(rec, ref)

	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	req := provider.Request{
		Messages: []message.Message{
			message.NewUserText("here is my key: " + secret),
		},
	}

	if _, err := sp.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream() err = %v", err)
	}

	got := rec.lastReq.Messages[0].TextContent()
	if strings.Contains(got, secret) {
		t.Errorf("secret leaked to inner provider: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got %q", got)
	}
}

func TestSafeProvider_RedactsSystemPrompt(t *testing.T) {
	rec := &recordingProvider{name: "rec"}
	ref := new(FirewallRef)
	ref.Set(NewFirewall(FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 4.5,
	}))
	sp := WrapProvider(rec, ref)

	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	req := provider.Request{
		SystemPrompt: "operator key " + secret,
	}

	if _, err := sp.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream() err = %v", err)
	}

	if strings.Contains(rec.lastReq.SystemPrompt, secret) {
		t.Errorf("secret leaked in system prompt: %q", rec.lastReq.SystemPrompt)
	}
	if !strings.Contains(rec.lastReq.SystemPrompt, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got %q", rec.lastReq.SystemPrompt)
	}
}

func TestSafeProvider_PassesThroughStreamError(t *testing.T) {
	sentinel := fmt.Errorf("provider exploded")
	rec := &recordingProvider{name: "rec", streamErr: sentinel}
	sp := WrapProvider(rec, nil)

	_, err := sp.Stream(context.Background(), provider.Request{})
	if err != sentinel {
		t.Errorf("Stream() err = %v, want %v", err, sentinel)
	}
}

func TestSafeProvider_PassesThroughName(t *testing.T) {
	rec := &recordingProvider{name: "anthropic"}
	sp := WrapProvider(rec, nil)
	if got := sp.Name(); got != "anthropic" {
		t.Errorf("Name() = %q, want %q", got, "anthropic")
	}
}

func TestSafeProvider_PassesThroughDefaultModel(t *testing.T) {
	rec := &recordingProvider{name: "rec"}
	sp := WrapProvider(rec, nil)
	if got := sp.DefaultModel(); got != "rec-model" {
		t.Errorf("DefaultModel() = %q, want %q", got, "rec-model")
	}
}

func TestSafeProvider_PassesThroughModels(t *testing.T) {
	rec := &recordingProvider{name: "rec"}
	sp := WrapProvider(rec, nil)
	models, err := sp.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() err = %v", err)
	}
	if len(models) != 1 || models[0].ID != "rec-model" {
		t.Errorf("Models() = %+v, want one model rec-model", models)
	}
}

func TestSafeProvider_SatisfiesProviderInterface(t *testing.T) {
	// Compile-time check that *SafeProvider implements provider.Provider.
	var _ provider.Provider = (*SafeProvider)(nil)
}
