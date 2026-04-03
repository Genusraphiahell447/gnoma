package tool

// Result is the output of a tool execution.
type Result struct {
	// Output is the text content returned to the LLM.
	Output string
	// Metadata carries optional structured data (exit code, file path, match count, etc.).
	Metadata map[string]any
}
