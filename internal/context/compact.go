package context

import "somegit.dev/Owlibou/gnoma/internal/message"

// safeSplitPoint adjusts a compaction split index to avoid orphaning tool
// results. If history[target] is a tool-result message, it walks backward
// until it finds a message that is not a tool result, so the assistant message
// that issued the tool calls stays in the "recent" window alongside its results.
//
// target is the index of the first message to keep in the recent window.
// Returns an adjusted index guaranteed to keep tool-call/tool-result pairs together.
func safeSplitPoint(history []message.Message, target int) int {
	if target <= 0 || len(history) == 0 {
		return 0
	}
	if target >= len(history) {
		target = len(history) - 1
	}
	idx := target
	for idx > 0 && hasToolResults(history[idx]) {
		idx--
	}
	return idx
}

// hasToolResults reports whether msg contains any ContentToolResult blocks.
func hasToolResults(msg message.Message) bool {
	for _, c := range msg.Content {
		if c.Type == message.ContentToolResult {
			return true
		}
	}
	return false
}
