package room

import (
	"errors"
	"sort"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
)

// ErrNoMonitor is the sentinel StaleMembers returns for a nil hb.
// Test with errors.Is, matching every other room sentinel.
var ErrNoMonitor = errors.New("room: heartbeat monitor is required")

// StaleMembers returns the sorted current roster members that
// hb.Dead(now) also reports. A nil hb returns nil and ErrNoMonitor,
// checked before r touches its own lock. The roster, not hb, is the
// source of truth for membership: a member Remove already dropped
// from the roster never appears, even if hb still tracks a stale
// beat for it. A member with no recorded beat never appears either;
// hb.Dead only reports an id that has beaten at least once.
// StaleMembers takes its own read lock around the roster read and the
// intersection, so a concurrent Remove, Admit, or Promote cannot
// interleave with the computed result.
func (r *Room) StaleMembers(hb *heartbeat.Monitor, now time.Time) ([]string, error) {
	if hb == nil {
		return nil, ErrNoMonitor
	}
	dead := make(map[string]struct{})
	for _, id := range hb.Dead(now) {
		dead[id] = struct{}{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.members))
	for id := range r.members {
		if _, ok := dead[id]; ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}
