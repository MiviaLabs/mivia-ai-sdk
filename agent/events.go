package agent

import "github.com/MiviaLabs/mivia-ai-sdk/events"

// MessageDeliveredEvent is the event kind EmitMessageDelivered emits
// after a delivered Message verifies. See translator.go.
const MessageDeliveredEvent events.Name = "agent.message_delivered"

// MessageAckedEvent is the event kind EmitMessageAcked emits after an
// Ack validates. See translator.go.
const MessageAckedEvent events.Name = "agent.message_acked"

// ThreadVerifiedEvent is the event kind EmitThreadVerified emits
// after a thread verifies. See translator.go.
const ThreadVerifiedEvent events.Name = "agent.thread_verified"
