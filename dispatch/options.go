package dispatch

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// Handler resolves one received message into a restatement. New's
// caller supplies a Resolve func that looks one up per message.
// Implementations receive an already-verified, already-admitted
// message.
type Handler interface {
	Handle(ctx context.Context, m envelope.Message) (string, error)
}

// Options configures New. ID, Room, and Resolve are required.
type Options struct {
	// ID is this endpoint's identity; it becomes each ack's From.
	ID string
	// Room gates admission: only a signed message from a member,
	// addressed only to members, and naming this room, is admitted.
	Room *room.Room
	// Resolve looks up the Handler that owns an admitted message.
	Resolve func(ctx context.Context, m envelope.Message) (Handler, error)
	// Bus receives MessageDeliveredEvent and MessageAckedEvent. Built
	// and subscribed when nil.
	Bus *events.Bus
	// MaxBodyBytes caps one request body. Zero resolves to
	// DefaultMaxBodyBytes; a negative value fails Validate. A body
	// past the cap answers 400 with ErrBadRequest.
	MaxBodyBytes int64
	// Ledger provides replay protection over the receive ladder. Built
	// as a bounded in-memory ledger, sharing Bus for its events, when
	// nil.
	Ledger *ledger.Ledger
	// ReplayLease bounds one line's replay claim. Zero resolves to
	// DefaultReplayLease. A negative value, or a value under one
	// second, fails Validate. Size this above Handler.Handle's
	// expected p99 latency; see the warning in the Scope section of
	// docs/plans/dispatch.md.
	ReplayLease time.Duration
	// ReplayCapacity caps the entry count of the ledger New builds
	// internally when Ledger is nil. Zero resolves to
	// DefaultReplayCapacity. A negative value fails Validate. Ignored
	// when Ledger is set; the caller-supplied Ledger owns its own
	// Store's capacity.
	ReplayCapacity int
}

// DefaultMaxBodyBytes caps one NDJSON request body when
// Options.MaxBodyBytes is zero. It bounds the memory one request can
// commit before any line runs the receive ladder.
const DefaultMaxBodyBytes int64 = 1 << 20

// DefaultReplayLease bounds one line's replay claim when
// Options.ReplayLease is zero. This is not a crash-detection timeout:
// size it above Handler.Handle's expected p99 latency, or a slow
// handler's own replay can re-run work before its first claim
// completes. See the Scope section warning in docs/plans/dispatch.md.
const DefaultReplayLease = 30 * time.Second

// DefaultReplayCapacity caps the entry count of the ledger New builds
// internally when Options.Ledger is nil. A caller-supplied Ledger is
// not bounded by this constant; the caller owns its Store's capacity.
const DefaultReplayCapacity = 100000

// minReplayLease is a sanity floor, not a latency guess: Validate
// cannot know a caller's handler latency, so it rejects only a value
// implausible for any handler, catching a unit-confusion bug
// (milliseconds mistaken for seconds) independent of real latency
// data.
const minReplayLease = time.Second

// Sentinel errors for New and Endpoint.Handler; test with errors.Is.
var (
	ErrNoID       = errors.New("dispatch: endpoint id is required")
	ErrNoRoom     = errors.New("dispatch: room is required")
	ErrNoResolve  = errors.New("dispatch: resolve func is required")
	ErrBadMethod  = errors.New("dispatch: POST required")
	ErrBadRequest = errors.New("dispatch: request body read failed")
	ErrBadMaxBody = errors.New("dispatch: max body bytes must not be negative")
	// ErrBadReplayLease reports a negative Options.ReplayLease, a
	// ReplayLease under one second, or a negative
	// Options.ReplayCapacity. Options.Validate returns this sentinel
	// for any of the three.
	ErrBadReplayLease = errors.New("dispatch: replay lease and capacity must not be negative")
	// ErrReplay reports a message the ledger already admitted: a
	// completed, failed, or blocked key, or a key still claimed by an
	// in-flight duplicate. Endpoint.Handler answers this with a
	// "replay:" error line instead of running resolve or handle again.
	ErrReplay = errors.New("dispatch: message already processed")
)

// Validate checks ID, Room, Resolve, MaxBodyBytes, ReplayLease, and
// ReplayCapacity, in that order, and returns the first sentinel that
// fails. A nonzero ReplayLease under one second fails Validate: this
// is a sanity floor against a unit-confusion bug, not a floor tied to
// any handler's real latency.
func (o Options) Validate() error {
	if strings.TrimSpace(o.ID) == "" {
		return ErrNoID
	}
	if o.Room == nil {
		return ErrNoRoom
	}
	if o.Resolve == nil {
		return ErrNoResolve
	}
	if o.MaxBodyBytes < 0 {
		return ErrBadMaxBody
	}
	if o.ReplayLease < 0 || (o.ReplayLease > 0 && o.ReplayLease < minReplayLease) {
		return ErrBadReplayLease
	}
	if o.ReplayCapacity < 0 {
		return ErrBadReplayLease
	}
	return nil
}

// New validates opts, builds a Bus when opts.Bus is nil, subscribes a
// no-op handler for MessageDeliveredEvent and MessageAckedEvent on the
// resolved bus, and returns the wired Endpoint. The subscription keeps
// EmitMessageDelivered and EmitMessageAcked from ever seeing an
// unsubscribed-name error.
func New(opts Options) (*Endpoint, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	bus := opts.Bus
	if bus == nil {
		bus = events.New()
	}
	noop := func(context.Context, events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent} {
		if err := bus.Subscribe(name, noop); err != nil {
			return nil, err
		}
	}
	maxBody := opts.MaxBodyBytes
	if maxBody == 0 {
		maxBody = DefaultMaxBodyBytes
	}
	led := opts.Ledger
	if led == nil {
		capacity := opts.ReplayCapacity
		if capacity == 0 {
			capacity = DefaultReplayCapacity
		}
		store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: capacity})
		if err != nil {
			return nil, err
		}
		led, err = ledger.New(store, bus)
		if err != nil {
			return nil, err
		}
	}
	lease := opts.ReplayLease
	if lease == 0 {
		lease = DefaultReplayLease
	}
	return &Endpoint{
		id:      opts.ID,
		room:    opts.Room,
		resolve: opts.Resolve,
		bus:     bus,
		maxBody: maxBody,
		taskOpts: taskrun.Options{
			Ledger: led,
			Actor:  ledger.Actor(opts.ID),
			Owner:  ledger.OwnerID(opts.ID),
			Lease:  lease,
		},
	}, nil
}
