// RoomTool manages room membership from a tool call.

package subagent

import (
	"context"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// RoomCommand is the JSON wire form of one room tool call.
type RoomCommand struct {
	Op string `json:"op"`
	ID string `json:"id"`
	By string `json:"by"`
}

// Room operation constants.
const (
	// OpAdmit adds a member, as By or the bound actor.
	OpAdmit = "admit"
	// OpRemove removes a member, as By or the bound actor.
	OpRemove = "remove"
	// OpPromote promotes a member to admin, as By or the bound actor.
	OpPromote = "promote"
	// OpMembers lists every member id, comma-joined.
	OpMembers = "members"
	// OpIsMember reports whether ID holds membership.
	OpIsMember = "ismember"
)

// RoomTool returns a tool bound to one room and acting actor. The
// By field overrides the bound actor per call.
func RoomTool(name string, r *room.Room, actor string) tools.Tool {
	return &roomTool{name: name, room: r, actor: actor}
}

// roomTool adapts one room to the tools.Tool interface.
type roomTool struct {
	name  string
	room  *room.Room
	actor string
}

// Name returns the registry name.
func (t *roomTool) Name() string { return t.name }

// Run executes one decoded command.
func (t *roomTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	var cmd RoomCommand
	if err := decodeCommand(stringValue(in), &cmd); err != nil {
		return tools.Out{}, t.bad()
	}
	by := cmd.By
	if by == "" {
		by = t.actor
	}
	switch cmd.Op {
	case OpAdmit:
		return t.ok(t.room.Admit(cmd.ID, by))
	case OpRemove:
		return t.ok(t.room.Remove(cmd.ID, by))
	case OpPromote:
		return t.ok(t.room.Promote(cmd.ID, by))
	case OpMembers:
		return tools.Out{Value: strings.Join(t.room.Members(), ",")}, nil
	case OpIsMember:
		if t.room.IsMember(cmd.ID) {
			return tools.Out{Value: "true"}, nil
		}
		return tools.Out{Value: "false"}, nil
	default:
		return tools.Out{}, t.bad()
	}
}

// ok maps a room error onto a plain ok result.
func (t *roomTool) ok(err error) (tools.Out, error) {
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: "ok"}, nil
}

// bad builds the bad-command sentinel.
func (t *roomTool) bad() error {
	return badCommand(t.name)
}
