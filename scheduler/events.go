package scheduler

import "github.com/MiviaLabs/mivia-ai-sdk/events"

// JobFailedEvent is the event kind Run emits when a Job returns a
// non-nil error. See run.go for the Data shape.
const JobFailedEvent events.Name = "scheduler.job_failed"
