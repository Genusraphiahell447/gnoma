package provider_test

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestCapabilities_SupportsThinking(t *testing.T) {
	tests := []struct {
		name string
		caps provider.Capabilities
		want bool
	}{
		{"no modes", provider.Capabilities{}, false},
		{"single mode", provider.Capabilities{ThinkingModes: []provider.EffortLevel{provider.EffortHigh}}, true},
		{"all modes", provider.Capabilities{ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.caps.SupportsThinking(); got != tc.want {
				t.Errorf("SupportsThinking() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCapabilities_SupportsEffort(t *testing.T) {
	caps := provider.Capabilities{
		ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortHigh},
	}

	tests := []struct {
		level provider.EffortLevel
		want  bool
	}{
		{provider.EffortAuto, true},    // EffortAuto always passes
		{provider.EffortLow, true},     // explicitly listed
		{provider.EffortMedium, false}, // not listed
		{provider.EffortHigh, true},    // explicitly listed
	}
	for _, tc := range tests {
		if got := caps.SupportsEffort(tc.level); got != tc.want {
			t.Errorf("SupportsEffort(%v) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestCapabilities_SupportsEffort_NoThinking(t *testing.T) {
	caps := provider.Capabilities{}

	if !caps.SupportsEffort(provider.EffortAuto) {
		t.Error("EffortAuto should always be supported even with no thinking modes")
	}
	if caps.SupportsEffort(provider.EffortLow) {
		t.Error("EffortLow should not be supported with no thinking modes")
	}
}

func TestEffortLevel_String(t *testing.T) {
	tests := []struct {
		level provider.EffortLevel
		want  string
	}{
		{provider.EffortAuto, "auto"},
		{provider.EffortLow, "low"},
		{provider.EffortMedium, "medium"},
		{provider.EffortHigh, "high"},
	}
	for _, tc := range tests {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("EffortLevel(%d).String() = %q, want %q", tc.level, got, tc.want)
		}
	}
}
