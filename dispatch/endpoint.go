package dispatch

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// Endpoint receives NDJSON envelope messages and answers with NDJSON
// acks. Build one with New.
type Endpoint struct {
	id      string
	room    *room.Room
	resolve func(ctx context.Context, m envelope.Message) (Handler, error)
	bus     *events.Bus
}

// Handler serves POST requests with NDJSON bodies. A non-POST method
// answers 405 with ErrBadMethod. An unreadable body answers 400 with
// ErrBadRequest. Each request line runs the receive ladder and
// contributes one reply line, an ack or a JSON error object; the
// stream stays open across a per-line failure.
func (e *Endpoint) Handler() http.Handler {
	return http.HandlerFunc(e.serveHTTP)
}

// serveHTTP implements the Endpoint.Handler contract.
func (e *Endpoint) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, ErrBadMethod.Error(), http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, ErrBadRequest.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		out := e.processLine(ctx, line)
		w.Write(out)
		w.Write([]byte("\n"))
	}
}
