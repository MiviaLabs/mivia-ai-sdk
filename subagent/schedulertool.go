// SchedulerTool schedules a bound job from a tool call.

package subagent

import (
	"context"
	"strconv"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// SchedulerCommand is the JSON wire form of one scheduler tool call.
type SchedulerCommand struct {
	Op      string `json:"op"`
	ID      string `json:"id"`
	EveryMs int64  `json:"every_ms"`
	AtMs    int64  `json:"at_ms"`
}

// Scheduler operation constants.
const (
	// OpEvery schedules the bound job on a fixed interval.
	OpEvery = "every"
	// OpAt schedules the bound job at fixed times.
	OpAt = "at"
	// OpCancel cancels one scheduled job.
	OpCancel = "cancel"
)

// SchedulerTool returns a tool bound to one scheduler and one job.
// The job closure is caller code; the tool only places it on a
// schedule or removes it.
func SchedulerTool(name string, s *scheduler.Scheduler, job scheduler.Job) tools.Tool {
	return &schedulerTool{name: name, sched: s, job: job}
}

// schedulerTool adapts one scheduler and job to the tools.Tool
// interface.
type schedulerTool struct {
	name  string
	sched *scheduler.Scheduler
	job   scheduler.Job
}

// Name returns the registry name.
func (t *schedulerTool) Name() string { return t.name }

// Run executes one decoded command.
func (t *schedulerTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	var cmd SchedulerCommand
	if err := decodeCommand(stringValue(in), &cmd); err != nil {
		return tools.Out{}, badCommand(t.name)
	}
	switch cmd.Op {
	case OpEvery:
		return t.add(cmd.ID, scheduler.Every(time.Duration(cmd.EveryMs)*time.Millisecond))
	case OpAt:
		return t.add(cmd.ID, scheduler.At(time.UnixMilli(cmd.AtMs)))
	case OpCancel:
		if t.sched.Remove(cmd.ID) {
			return tools.Out{Value: "removed"}, nil
		}
		return tools.Out{Value: "absent"}, nil
	default:
		return tools.Out{}, badCommand(t.name)
	}
}

// add registers the bound job and reports the id back.
func (t *schedulerTool) add(id string, sched scheduler.Schedule) (tools.Out, error) {
	if err := t.sched.Add(id, sched, t.job); err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: strconv.Quote(id)}, nil
}
