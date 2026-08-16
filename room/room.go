// Package room manages standing groups for envelope messages: a roster
// with roles, moderator-gated admission, and a gate that admits an
// envelope.Message only when its signer is a member. The room carries
// the roster that envelope.Message.Room only names. See envelope/ for
// the wire format and ../docs/protocol-design.md for the rationale.
package room

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Role is a member's power in a Room.
type Role string

const (
	RoleModerator Role = "moderator" // can admit, remove, promote
	RoleMember    Role = "member"    // can post and leave
)

// Sentinel errors for room operations; test with errors.Is.
var (
	ErrNotMember     = errors.New("not a room member")
	ErrNotModerator  = errors.New("not a room moderator")
	ErrAlreadyMember = errors.New("already a room member")
	ErrLastModerator = errors.New("cannot remove the last moderator")
	ErrWrongRoom     = errors.New("message names a different room")
	ErrUnsigned      = errors.New("unsigned message cannot be admitted")
)

// Room is a named standing group with a role roster. Safe for
// concurrent use.
type Room struct {
	id      string
	mu      sync.RWMutex
	members map[string]Role
}

// New creates a Room with founder as its first moderator. Both are
// required; a room without a moderator cannot change membership.
func New(id, founder string) (*Room, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("room id is required")
	}
	if strings.TrimSpace(founder) == "" {
		return nil, errors.New("founder is required")
	}
	return &Room{id: id, members: map[string]Role{founder: RoleModerator}}, nil
}

// ID returns the room name that envelope.Message.Room must carry.
func (r *Room) ID() string { return r.id }

// Admit adds id as a member. by must be a moderator.
func (r *Room) Admit(id, by string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.moderatorLocked(by); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("member id is required")
	}
	if _, ok := r.members[id]; ok {
		return ErrAlreadyMember
	}
	r.members[id] = RoleMember
	return nil
}

// Remove drops id from the roster. by must be a moderator. The last
// moderator cannot be removed.
func (r *Room) Remove(id, by string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.moderatorLocked(by); err != nil {
		return err
	}
	role, ok := r.members[id]
	if !ok {
		return ErrNotMember
	}
	if role == RoleModerator && r.moderatorsLocked() == 1 {
		return ErrLastModerator
	}
	delete(r.members, id)
	return nil
}

// Leave drops id from the roster by its own choice. The last moderator
// cannot leave.
func (r *Room) Leave(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	role, ok := r.members[id]
	if !ok {
		return ErrNotMember
	}
	if role == RoleModerator && r.moderatorsLocked() == 1 {
		return ErrLastModerator
	}
	delete(r.members, id)
	return nil
}

// Promote raises a member to moderator. by must be a moderator.
func (r *Room) Promote(id, by string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.moderatorLocked(by); err != nil {
		return err
	}
	if _, ok := r.members[id]; !ok {
		return ErrNotMember
	}
	r.members[id] = RoleModerator
	return nil
}

// IsMember reports whether id is on the roster.
func (r *Room) IsMember(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.members[id]
	return ok
}

// Members returns the sorted roster.
func (r *Room) Members() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.members))
	for id := range r.members {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Accepts gates an envelope message: it must name this room, carry a
// valid signature from a roster member, and address only members.
// Accepts verifies the signature itself; a caller does not need to.
func (r *Room) Accepts(m envelope.Message) error {
	if m.Room != r.id {
		return fmt.Errorf("%w: %q", ErrWrongRoom, m.Room)
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if m.Signer == "" {
		return ErrUnsigned
	}
	if err := m.VerifySignature(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsigned, err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.members[m.Signer]; !ok {
		return fmt.Errorf("%w: signer", ErrNotMember)
	}
	for _, to := range m.To {
		if _, ok := r.members[to]; !ok {
			return fmt.Errorf("%w: recipient %q", ErrNotMember, to)
		}
	}
	return nil
}

// moderatorLocked checks the by identity. Caller holds the lock.
func (r *Room) moderatorLocked(by string) error {
	if r.members[by] != RoleModerator {
		return ErrNotModerator
	}
	return nil
}

// moderatorsLocked counts moderators. Caller holds the lock.
func (r *Room) moderatorsLocked() int {
	n := 0
	for _, role := range r.members {
		if role == RoleModerator {
			n++
		}
	}
	return n
}
