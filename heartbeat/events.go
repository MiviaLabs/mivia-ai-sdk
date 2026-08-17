package heartbeat

import "github.com/MiviaLabs/mivia-ai-sdk/events"

// MissedEvent is the event kind a caller emits after it observes a
// dead id via Dead. Heartbeat owns the name; it never emits the event.
const MissedEvent events.Name = "heartbeat.missed"
