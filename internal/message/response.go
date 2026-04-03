package message

// Response wraps a completed assistant turn.
type Response struct {
	Message    Message
	StopReason StopReason
	Usage      Usage
	Model      string
}
