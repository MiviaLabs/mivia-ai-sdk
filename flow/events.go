package flow

import "github.com/MiviaLabs/mivia-ai-sdk/events"

// StepCompletedEvent is the event kind the runner emits after a step
// resolves. See machine/events.go for the machine counterpart.
const StepCompletedEvent events.Name = "flow.step_completed"
