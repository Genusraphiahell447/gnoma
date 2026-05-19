package router

import "testing"

func TestParseTaskType(t *testing.T) {
	cases := []struct {
		input string
		want  TaskType
	}{
		{"Debug", TaskDebug},
		{"debug", TaskDebug},
		{"DEBUG", TaskDebug},
		{"Explain", TaskExplain},
		{"explain", TaskExplain},
		{"Generation", TaskGeneration},
		{"generation", TaskGeneration},
		{"Refactor", TaskRefactor},
		{"refactor", TaskRefactor},
		{"UnitTest", TaskUnitTest},
		{"unit_test", TaskUnitTest},
		{"unitTest", TaskUnitTest},
		{"Boilerplate", TaskBoilerplate},
		{"boilerplate", TaskBoilerplate},
		{"Planning", TaskPlanning},
		{"planning", TaskPlanning},
		{"Orchestration", TaskOrchestration},
		{"orchestration", TaskOrchestration},
		{"SecurityReview", TaskSecurityReview},
		{"security_review", TaskSecurityReview},
		{"Review", TaskReview},
		{"review", TaskReview},
		// unknown falls back to TaskGeneration
		{"", TaskGeneration},
		{"unknown", TaskGeneration},
		{"gibberish", TaskGeneration},
	}

	for _, tc := range cases {
		got := ParseTaskType(tc.input)
		if got != tc.want {
			t.Errorf("ParseTaskType(%q) = %s, want %s", tc.input, got, tc.want)
		}
	}
}

func TestClassifyTask_TrivialPromptDropsRequiresTools(t *testing.T) {
	// Short, knowledge-only prompts must opt out of RequiresTools so the
	// SLM arm (ToolUse=false) is feasible.
	cases := []struct {
		prompt string
	}{
		{"what is 2+2"},
		{"what is a closure"},
		{"explain a goroutine"},
		{"how does map work"},
	}
	for _, tc := range cases {
		got := ClassifyTask(tc.prompt)
		if got.RequiresTools {
			t.Errorf("ClassifyTask(%q).RequiresTools = true, want false (trivial prompt)", tc.prompt)
		}
	}
}

func TestClassifyTask_FileVerbsKeepRequiresTools(t *testing.T) {
	// Even short prompts must keep RequiresTools=true when they reference
	// a file/shell action — those genuinely need tool execution.
	cases := []struct {
		prompt string
	}{
		{"read /etc/hosts"},
		{"list files in /tmp"},
		{"run tests"},
		{"show me the diff"},
	}
	for _, tc := range cases {
		got := ClassifyTask(tc.prompt)
		if !got.RequiresTools {
			t.Errorf("ClassifyTask(%q).RequiresTools = false, want true (file/shell verb)", tc.prompt)
		}
	}
}

func TestClassifyTask_LongPromptsKeepRequiresTools(t *testing.T) {
	// Prompts longer than 12 words shouldn't be treated as trivial even
	// if they don't reference file actions explicitly.
	prompt := "I want you to think through how a generic interpreter handles closures across multiple call sites and many layers of indirection"
	got := ClassifyTask(prompt)
	if !got.RequiresTools {
		t.Errorf("long prompt got RequiresTools=false; want true")
	}
}
