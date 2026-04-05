package agent

import "fmt"

const maxOutputChars = 2000

// truncateOutput truncates output to max characters, appending a note with the original length.
func truncateOutput(output string, max int) string {
	if len(output) <= max {
		return output
	}
	return output[:max] + fmt.Sprintf("\n\n[truncated — full output was %d chars]", len(output))
}
