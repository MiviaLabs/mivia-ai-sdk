package dispatch

import (
	"context"
	"errors"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
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
}

// Sentinel errors for New and Endpoint.Handler; test with errors.Is.
var (
	ErrNoID       = errors.New("dispatch: endpoint id is required")
	ErrNoRoom     = errors.New("dispatch: room is required")
	ErrNoResolve  = errors.New("dispatch: resolve func is required")
	ErrBadMethod  = errors.New("dispatch: POST required")
	ErrBadRequest = errors.New("dispatch: request body read failed")
)

// Validate checks ID, Room, and Resolve, in that order, and returns
// the first sentinel that fails.
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
	return &Endpoint{
		id:      opts.ID,
		room:    opts.Room,
		resolve: opts.Resolve,
		bus:     bus,
	}, nil
}
