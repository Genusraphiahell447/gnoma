package security_test

import (
	"context"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// External test package (security_test) so we exercise the public surface
// the way main.go does — WrapProvider in front of an arm, router.Stream
// dispatches through it, the inner provider sees redacted content.

type capturingProvider struct {
	name    string
	lastReq provider.Request
}

func (p *capturingProvider) Name() string         { return p.name }
func (p *capturingProvider) DefaultModel() string { return "cap-model" }
func (p *capturingProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "cap-model", Name: "cap-model", Provider: p.name}}, nil
}
func (p *capturingProvider) Stream(_ context.Context, req provider.Request) (stream.Stream, error) {
	p.lastReq = req
	return &nopStream{}, nil
}

type nopStream struct{}

func (s *nopStream) Next() bool            { return false }
func (s *nopStream) Current() stream.Event { return stream.Event{} }
func (s *nopStream) Err() error            { return nil }
func (s *nopStream) Close() error          { return nil }

func TestRouterArmWrappedWithSafeProvider_RedactsBeforeDelivery(t *testing.T) {
	cap := &capturingProvider{name: "cap"}
	ref := new(security.FirewallRef)
	ref.Set(security.NewFirewall(security.FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 4.5,
	}))

	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("cap", "cap-model"),
		Provider:     security.WrapProvider(cap, ref),
		ModelName:    "cap-model",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: false},
	})

	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	req := provider.Request{
		Messages: []message.Message{
			message.NewUserText("here is my key: " + secret),
		},
	}

	s, decision, err := rtr.Stream(context.Background(), router.Task{Type: router.TaskReview}, req)
	if err != nil {
		t.Fatalf("router.Stream err = %v", err)
	}
	defer func() { _ = s.Close() }()
	decision.Commit(0)

	got := cap.lastReq.Messages[0].TextContent()
	if strings.Contains(got, secret) {
		t.Errorf("secret reached inner provider via router: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker after router dispatch, got %q", got)
	}
}

func TestRouterArmWrappedBeforeFirewallSet_PassesThroughUntilSet(t *testing.T) {
	// Mirrors the construction order in main.go: arm is wrapped with a
	// FirewallRef whose pointer isn't installed yet. A Stream call in that
	// state must pass through unmodified; once Set fires, subsequent calls
	// are scanned.
	cap := &capturingProvider{name: "cap"}
	ref := new(security.FirewallRef) // not Set yet

	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("cap", "cap-model"),
		Provider:     security.WrapProvider(cap, ref),
		ModelName:    "cap-model",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: false},
	})

	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	req := provider.Request{
		Messages: []message.Message{message.NewUserText(secret)},
	}

	// First call — ref unset, must pass through.
	s, decision, err := rtr.Stream(context.Background(), router.Task{Type: router.TaskReview}, req)
	if err != nil {
		t.Fatalf("router.Stream (pre-Set) err = %v", err)
	}
	_ = s.Close()
	decision.Commit(0)

	if got := cap.lastReq.Messages[0].TextContent(); !strings.Contains(got, secret) {
		t.Fatalf("pre-Set call was modified: %q", got)
	}

	// Now install the firewall and call again.
	ref.Set(security.NewFirewall(security.FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 4.5,
	}))

	s, decision, err = rtr.Stream(context.Background(), router.Task{Type: router.TaskReview}, req)
	if err != nil {
		t.Fatalf("router.Stream (post-Set) err = %v", err)
	}
	_ = s.Close()
	decision.Commit(0)

	got := cap.lastReq.Messages[0].TextContent()
	if strings.Contains(got, secret) {
		t.Errorf("secret reached inner provider after Set: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] after Set, got %q", got)
	}
}
