package runconfig

import (
	"errors"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Sentinel errors; test with errors.Is.
var (
	// ErrUnknownTool names an external tool absent from External.
	ErrUnknownTool = errors.New("runconfig: unknown tool")
	// ErrUnknownInternal names an internal Kind absent from Blocks.
	ErrUnknownInternal = errors.New("runconfig: unknown internal tool")
	// ErrBadDocument names any malformed or rejected document shape.
	ErrBadDocument = errors.New("runconfig: bad document")
)

// Kind names one subagent internal tool family. A step's internal
// binding names one Kind; the caller builds the tool through the
// matching subagent helper and sets it on Blocks.
type Kind string

// The internal tool kinds a document may name.
const (
	FlowKind             Kind = "flow"
	LedgerKind           Kind = "ledger"
	MemoryKind           Kind = "memory"
	RoomKind             Kind = "room"
	SchedulerKind        Kind = "scheduler"
	HeartbeatKind        Kind = "heartbeat"
	DiscoveryKind        Kind = "discovery"
	TriggerKind          Kind = "trigger"
	ChannelKind          Kind = "channel"
	ProviderKind         Kind = "provider"
	ProviderRegistryKind Kind = "providerregistry"
)

// kinds holds every Kind constant, for internal-name validation.
var kinds = map[Kind]bool{
	FlowKind:             true,
	LedgerKind:           true,
	MemoryKind:           true,
	RoomKind:             true,
	SchedulerKind:        true,
	HeartbeatKind:        true,
	DiscoveryKind:        true,
	TriggerKind:          true,
	ChannelKind:          true,
	ProviderKind:         true,
	ProviderRegistryKind: true,
}

// Blocks holds one tools.Tool per internal Kind. The caller builds
// each tool through the matching subagent helper and sets it before
// Runner. Safe for concurrent use.
type Blocks struct {
	mu sync.Mutex
	m  map[Kind]tools.Tool
}

// NewBlocks returns an empty Blocks.
func NewBlocks() *Blocks {
	return &Blocks{m: make(map[Kind]tools.Tool)}
}

// Set registers t under kind, replacing any earlier tool.
func (b *Blocks) Set(kind Kind, t tools.Tool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[kind] = t
}

// get resolves kind to its tool. Returns false when kind is absent.
func (b *Blocks) get(kind Kind) (tools.Tool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.m[kind]
	return t, ok
}

// Binding ties one step to one tool source. Exactly one of Tool and
// Internal's Kind is meaningful, per Internal.
type Binding struct {
	// Step is the bound step's ID.
	Step string
	// Tool names the external tool. Set only when Internal is false.
	Tool string
	// Kind names the internal family. Set only when Internal is true.
	Kind Kind
	// Internal separates an internal binding from an external one.
	Internal bool
}
