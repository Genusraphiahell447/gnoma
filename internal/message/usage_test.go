package message

import "testing"

func TestUsage_TotalTokens(t *testing.T) {
	u := Usage{InputTokens: 100, OutputTokens: 50}
	if got := u.TotalTokens(); got != 150 {
		t.Errorf("TotalTokens() = %d, want 150", got)
	}
}

func TestUsage_TotalTokens_Zero(t *testing.T) {
	var u Usage
	if got := u.TotalTokens(); got != 0 {
		t.Errorf("TotalTokens() = %d, want 0", got)
	}
}

func TestUsage_Add(t *testing.T) {
	u := Usage{
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     10,
		CacheCreationTokens: 5,
	}
	other := Usage{
		InputTokens:         200,
		OutputTokens:        80,
		CacheReadTokens:     20,
		CacheCreationTokens: 15,
	}

	u.Add(other)

	if u.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", u.InputTokens)
	}
	if u.OutputTokens != 130 {
		t.Errorf("OutputTokens = %d, want 130", u.OutputTokens)
	}
	if u.CacheReadTokens != 30 {
		t.Errorf("CacheReadTokens = %d, want 30", u.CacheReadTokens)
	}
	if u.CacheCreationTokens != 20 {
		t.Errorf("CacheCreationTokens = %d, want 20", u.CacheCreationTokens)
	}
}

func TestUsage_Add_Multiple(t *testing.T) {
	var total Usage
	turns := []Usage{
		{InputTokens: 100, OutputTokens: 50},
		{InputTokens: 200, OutputTokens: 80},
		{InputTokens: 150, OutputTokens: 60},
	}

	for _, turn := range turns {
		total.Add(turn)
	}

	if total.InputTokens != 450 {
		t.Errorf("InputTokens = %d, want 450", total.InputTokens)
	}
	if total.OutputTokens != 190 {
		t.Errorf("OutputTokens = %d, want 190", total.OutputTokens)
	}
	if total.TotalTokens() != 640 {
		t.Errorf("TotalTokens() = %d, want 640", total.TotalTokens())
	}
}
