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
	// ErrCallerBuilt names an internal Kind a document cannot build.
	// Load wraps it inside ErrBadDocument. Test with errors.Is.
	ErrCallerBuilt = errors.New("runconfig: internal kind stays caller-built")
)

// Kind names one subagent internal tool family. A step's internal
// binding names one Kind. The document's internal section builds six
// wireable Kinds at Load; the caller builds the rest through the
// matching subagent helper and sets them on Blocks. See
// runconfig/internal.go for the split.
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
	AsToolKind           Kind = "astool"
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
	AsToolKind:           true,
}

// Blocks holds one tools.Tool per internal Kind. Load fills the six
// wireable Kinds from the document's internal section; the caller
// sets the caller-built Kinds, or overrides a wireable one, through
// Set. Last write wins. Safe for concurrent use.
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
