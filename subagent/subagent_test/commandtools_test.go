package subagent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
)

// TestRoomToolManagesMembership drives admit, members, and ismember
// through the bound room.
func TestRoomToolManagesMembership(t *testing.T) {
	ctx := context.Background()
	r, err := room.New("ops", "founder")
	if err != nil {
		t.Fatalf("room.New: %v", err)
	}
	tool := subagent.RoomTool("room", r, "founder")
	if _, err := tool.Run(ctx, inString(`{"op":"admit","id":"new"}`)); err != nil {
		t.Fatalf("admit: %v", err)
	}
	out, err := tool.Run(ctx, inString(`{"op":"members"}`))
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if !strings.Contains(out.Value.(string), "founder") || !strings.Contains(out.Value.(string), "new") {
		t.Fatalf("members = %v, want founder and new", out.Value)
	}
	out, err = tool.Run(ctx, inString(`{"op":"ismember","id":"new"}`))
	if err != nil || out.Value != "true" {
		t.Fatalf("ismember = %v,%v, want true", out.Value, err)
	}
	out, err = tool.Run(ctx, inString(`{"op":"ismember","id":"nobody"}`))
	if err != nil || out.Value != "false" {
		t.Fatalf("ismember absent = %v,%v, want false", out.Value, err)
	}
}

// TestSchedulerToolSchedulesAndCancels proves a bound job lands on a
// future schedule and cancels by id.
func TestSchedulerToolSchedulesAndCancels(t *testing.T) {
	ctx := context.Background()
	s := scheduler.New()
	ran := make(chan struct{}, 1)
	job := func(ctx context.Context) error {
		ran <- struct{}{}
		return nil
	}
	tool := subagent.SchedulerTool("sched", s, job)
	future := time.Now().Add(time.Hour).UnixMilli()
	cmd, _ := json.Marshal(subagent.SchedulerCommand{
		Op: subagent.OpAt, ID: "nightly", AtMs: future,
	})
	if _, err := tool.Run(ctx, inString(string(cmd))); err != nil {
		t.Fatalf("at: %v", err)
	}
	cancel, _ := json.Marshal(subagent.SchedulerCommand{
		Op: subagent.OpCancel, ID: "nightly",
	})
	out, err := tool.Run(ctx, inString(string(cancel)))
	if err != nil || out.Value != "removed" {
		t.Fatalf("cancel = %v,%v, want removed", out.Value, err)
	}
	out, err = tool.Run(ctx, inString(string(cancel)))
	if err != nil || out.Value != "absent" {
		t.Fatalf("cancel again = %v,%v, want absent", out.Value, err)
	}
}

// TestHeartbeatToolReportsLiveness proves beat, alive, and dead
// against explicit clock values, with no sleeps.
func TestHeartbeatToolReportsLiveness(t *testing.T) {
	ctx := context.Background()
	m, err := heartbeat.New(time.Hour)
	if err != nil {
		t.Fatalf("heartbeat.New: %v", err)
	}
	tool := subagent.HeartbeatTool("beat", m)
	if _, err := tool.Run(ctx, inString(`{"op":"beat","id":"live"}`)); err != nil {
		t.Fatalf("beat: %v", err)
	}
	out, err := tool.Run(ctx, inString(`{"op":"alive","id":"live"}`))
	if err != nil || out.Value != "true" {
		t.Fatalf("alive = %v,%v, want true", out.Value, err)
	}
	out, err = tool.Run(ctx, inString(`{"op":"dead","id":"live"}`))
	if err != nil || out.Value != "" {
		t.Fatalf("dead = %v,%v, want empty", out.Value, err)
	}
}

// TestDiscoveryToolMatchesCard proves a parsed card's capability
// matches a need and reports none on a miss.
func TestDiscoveryToolMatchesCard(t *testing.T) {
	ctx := context.Background()
	tool := subagent.DiscoveryTool("cards")
	card, err := json.Marshal(discovery.Card{
		Name: "translator", Capabilities: []string{"text.translate"},
	})
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	cmd, _ := json.Marshal(subagent.DiscoveryCommand{
		Op: subagent.OpMatch, Card: string(card), Need: "text.translate",
	})
	out, err := tool.Run(ctx, inString(string(cmd)))
	if err != nil || out.Value != "text.translate" {
		t.Fatalf("match = %v,%v, want the capability", out.Value, err)
	}
	miss, _ := json.Marshal(subagent.DiscoveryCommand{
		Op: subagent.OpMatch, Card: string(card), Need: "image.draw",
	})
	out, err = tool.Run(ctx, inString(string(miss)))
	if err != nil || out.Value != "none" {
		t.Fatalf("miss = %v,%v, want none", out.Value, err)
	}
}
