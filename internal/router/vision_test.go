package router

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestFilterFeasible_RequiresVision_FiltersNonVisionArms(t *testing.T) {
	textOnly := &Arm{
		ID: NewArmID("ollama", "qwen2.5-coder:7b"),
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			Vision:        false,
			ContextWindow: 32768,
		},
	}
	visionArm := &Arm{
		ID: NewArmID("ollama", "llava:7b"),
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			Vision:        true,
			ContextWindow: 4096,
		},
	}
	arms := []*Arm{textOnly, visionArm}

	t.Run("no image: both arms feasible", func(t *testing.T) {
		task := Task{Type: TaskGeneration, RequiresTools: true, RequiresVision: false}
		got := filterFeasible(arms, task)
		if len(got) != 2 {
			t.Errorf("got %d arms, want 2", len(got))
		}
	})

	t.Run("image present: only vision arm feasible", func(t *testing.T) {
		task := Task{Type: TaskGeneration, RequiresTools: true, RequiresVision: true}
		got := filterFeasible(arms, task)
		if len(got) != 1 {
			t.Fatalf("got %d arms, want 1", len(got))
		}
		if got[0].ID != visionArm.ID {
			t.Errorf("selected arm = %s, want %s", got[0].ID, visionArm.ID)
		}
	})
}

func TestFilterFeasible_RequiresVision_FallbackAlsoFilters(t *testing.T) {
	// All arms unavailable for normal quality path; fallback path must
	// still respect RequiresVision (can't degrade to a text-only arm
	// when the model literally cannot see the image).
	textOnly := &Arm{
		ID: NewArmID("ollama", "qwen2.5:0.5b"), // tiny → low quality
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			Vision:        false,
			ContextWindow: 4096,
		},
	}
	arms := []*Arm{textOnly}

	task := Task{
		Type:           TaskGeneration,
		RequiresTools:  true,
		RequiresVision: true,
	}
	got := filterFeasible(arms, task)
	if len(got) != 0 {
		t.Errorf("got %d arms, want 0 — non-vision arm must not be selected even as fallback", len(got))
	}
}
