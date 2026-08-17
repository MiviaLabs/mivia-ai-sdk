package machine

import "github.com/MiviaLabs/mivia-ai-sdk/events"

// MoveEvent is the event kind a caller emits after a successful Fire.
// It is a machine concern, so its constant lives in this package.
const MoveEvent events.Name = "machine.move"
