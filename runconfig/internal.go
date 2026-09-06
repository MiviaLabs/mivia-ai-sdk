// Document-built internal tools: the internal section, its builders
// table, and the caller-built split. See docs/plans/runconfig.md's
// "Document-built internal tools" addendum.

package runconfig

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// wireInternal is the JSON form of one internal section entry. It
// holds every Kind's fields; each builder reads only its own. Unknown
// fields are ignored, matching the step grammar's rule.
type wireInternal struct {
	Timeout  string `json:"timeout"`
	MaxBytes int    `json:"max_bytes"`
	ID       string `json:"id"`
	Founder  string `json:"founder"`
	Actor    string `json:"actor"`
	Lease    string `json:"lease"`
}

// internalBuilder builds one document-internal tool. d carries the
// resolved plan and machine for the flow builder; cfg is the Kind's
// own section entry. A non-nil error is unwrapped text; buildInternal
// wraps it in ErrBadDocument.
type internalBuilder func(d *Definition, cfg wireInternal) (tools.Tool, error)

// builders maps every wireable Kind to its one subagent constructor
// call. This table is the mechanical link a step's internal binding
// resolves through. Each builder's name argument is the Kind's own
// string value, so tool errors name the family.
var builders = map[Kind]internalBuilder{
	DiscoveryKind: func(_ *Definition, _ wireInternal) (tools.Tool, error) {
		return subagent.DiscoveryTool(string(DiscoveryKind)), nil
	},
	FlowKind: func(d *Definition, _ wireInternal) (tools.Tool, error) {
		return subagent.FlowTool(string(FlowKind), d.Plan, d.Machine, nil), nil
	},
	HeartbeatKind: func(_ *Definition, cfg wireInternal) (tools.Tool, error) {
		timeout, err := parseInternalDuration(HeartbeatKind, "timeout", cfg.Timeout)
		if err != nil {
			return nil, err
		}
		monitor, err := heartbeat.New(timeout)
		if err != nil {
			return nil, err
		}
		return subagent.HeartbeatTool(string(HeartbeatKind), monitor), nil
	},
	LedgerKind: func(_ *Definition, cfg wireInternal) (tools.Tool, error) {
		if strings.TrimSpace(cfg.Actor) == "" {
			return nil, fmt.Errorf("internal %q: blank actor", LedgerKind)
		}
		lease, err := parseInternalDuration(LedgerKind, "lease", cfg.Lease)
		if err != nil {
			return nil, err
		}
		if lease <= 0 {
			return nil, fmt.Errorf("internal %q: lease must be positive", LedgerKind)
		}
		l, err := ledger.New(ledger.NewMemStore(), nil)
		if err != nil {
			return nil, err
		}
		return subagent.LedgerTool(string(LedgerKind), l, ledger.Actor(cfg.Actor), lease), nil
	},
	MemoryKind: func(_ *Definition, cfg wireInternal) (tools.Tool, error) {
		store, err := memory.New(cfg.MaxBytes)
		if err != nil {
			return nil, err
		}
		return subagent.MemoryTool(string(MemoryKind), store), nil
	},
	RoomKind: func(_ *Definition, cfg wireInternal) (tools.Tool, error) {
		if strings.TrimSpace(cfg.Actor) == "" {
			return nil, fmt.Errorf("internal %q: blank actor", RoomKind)
		}
		r, err := room.New(cfg.ID, cfg.Founder)
		if err != nil {
			return nil, err
		}
		return subagent.RoomTool(string(RoomKind), r, cfg.Actor), nil
	},
}

// callerBuilt names every Kind a JSON document cannot build. Each
// one's constructor takes a live value no document scalar encodes.
// The caller sets these through Blocks.Set.
var callerBuilt = map[Kind]bool{
	AsToolKind:           true,
	ChannelKind:          true,
	ProviderKind:         true,
	ProviderRegistryKind: true,
	SchedulerKind:        true,
	TriggerKind:          true,
}

// buildInternal builds one tool per internal section key and Sets it
// on d.Blocks. It walks the keys in sorted order, so rejections stay
// deterministic. A key no Kind constant names, or one naming a
// caller-built Kind, rejects the document. A duplicate section key
// follows encoding/json map semantics: the last value wins.
func buildInternal(d *Definition, section map[string]wireInternal) error {
	keys := make([]string, 0, len(section))
	for k := range section {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		k := Kind(name)
		if !kinds[k] {
			return fmt.Errorf("%w: unknown internal %q", ErrBadDocument, name)
		}
		if callerBuilt[k] {
			return fmt.Errorf("%w: %w: internal kind %q stays caller-built", ErrBadDocument, ErrCallerBuilt, name)
		}
		t, err := builders[k](d, section[name])
		if err != nil {
			return fmt.Errorf("%w: %s", ErrBadDocument, err.Error())
		}
		d.Blocks.Set(k, t)
	}
	return nil
}

// parseInternalDuration parses one internal config duration field. A
// bad string names the kind and the field.
func parseInternalDuration(k Kind, field, s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("internal %q config %q: %s", k, field, err)
	}
	return d, nil
}
