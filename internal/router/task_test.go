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
