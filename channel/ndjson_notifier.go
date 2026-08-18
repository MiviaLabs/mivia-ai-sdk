package channel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ndjsonScannerInitialBuffer and ndjsonScannerMaxBuffer size the
// bufio.Scanner readAnswer uses to read one NDJSON line: a 64 KB
// initial buffer, a 1 MB per-line cap, matching mivia-agent's own
// hub.connection.go readLoop and chat_repl_linemode.go sizing.
const (
	ndjsonScannerInitialBuffer = 64 * 1024
	ndjsonScannerMaxBuffer     = 1024 * 1024
)

// ndjsonQuestionType and ndjsonAnswerType are the wire "type" tag
// values NewNDJSONNotifier writes and expects, matching mivia-agent's
// snake_case wire convention.
const (
	ndjsonQuestionType = "question"
	ndjsonAnswerType   = "answer"
)

// ErrAnswerMismatch is returned when a decoded answer line's
// question_id does not match the Question.ID the same call sent. Test
// with errors.Is.
var ErrAnswerMismatch = errors.New("channel: ndjson: answer question id does not match question")

// ErrNotifierBusy is returned when a call arrives while another call
// on the same NewNDJSONNotifier closure already holds its internal
// lock. Test with errors.Is.
var ErrNotifierBusy = errors.New("channel: ndjson: notifier is busy with another call")

// ndjsonQuestionLine is the wire form NewNDJSONNotifier writes for one
// Question: a "type":"question" line with id, recipient, and payload
// fields, snake_case, matching mivia-agent's own ndjsonEvent
// convention. Internal to this file; Question keeps zero JSON tags.
type ndjsonQuestionLine struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
	Payload   string `json:"payload"`
}

// ndjsonAnswerLine is the wire form NewNDJSONNotifier reads back for
// one Answer: a "type":"answer" line with question_id, approved, and
// payload fields. Internal to this file; Answer keeps zero JSON tags.
type ndjsonAnswerLine struct {
	Type       string `json:"type"`
	QuestionID string `json:"question_id"`
	Approved   bool   `json:"approved"`
	Payload    string `json:"payload"`
}

// NewNDJSONNotifier builds a Notifier that writes one JSON-encoded
// question line to w and blocks reading one JSON-encoded answer line
// from r, in the newline-delimited-JSON shape mivia-agent's own
// stdio and hub protocols already use. ctx cancellation is honored
// during both phases: a blocked Write on w and a blocked Scan on r
// each return ctx.Err() promptly, without waiting for the underlying
// call to resolve.
//
// The returned Notifier serves one call at a time: a second call
// while one is already in flight, including one whose ctx was
// already canceled but whose background write or read has not yet
// resolved, returns ErrNotifierBusy immediately, without touching r
// or w. A caller that wires this closure into more than one
// concurrent call site (for example two independent agent.Run calls
// sharing one stdio pipe) must serialize its own calls or expect
// ErrNotifierBusy on overlap.
//
// After a ctx-canceled call, the closure stays locked until whichever
// phase was pending resolves: the write completes or errors, or, if
// the write had already finished, a line arrives, or the peer closes
// or errors r. Until then, every subsequent call on that closure
// returns ErrNotifierBusy. This is a deliberate limit, not an
// oversight: it is the price of never letting a second call start its
// own Write or Scan on w or r while a first call's background
// operation is still pending, which would corrupt the shared NDJSON
// stream. Closing w or r makes the stale operation return an error,
// releasing the lock; the closure is usable again after that.
func NewNDJSONNotifier(r io.Reader, w io.Writer) Notifier {
	var mu sync.Mutex
	return func(ctx context.Context, q Question) (Answer, error) {
		if !mu.TryLock() {
			return Answer{}, ErrNotifierBusy
		}
		if err := q.Validate(); err != nil {
			mu.Unlock()
			return Answer{}, err
		}
		if err := writeQuestion(ctx, r, w, q, mu.Unlock); err != nil {
			return Answer{}, err
		}
		ans, err := readAnswer(ctx, r, q.ID, mu.Unlock)
		if err != nil {
			return Answer{}, err
		}
		if err := ans.Validate(); err != nil {
			return Answer{}, err
		}
		return ans, nil
	}
}

// writeQuestion writes q to w as one NDJSON question line. The
// blocking encodeQuestionLine call runs in a goroutine so writeQuestion
// can also select on ctx.Done(), mirroring readAnswer's read-side
// pattern. release, always non-nil, runs exactly once per call:
//
//   - If ctx.Done() fires first, no further phase of this call has a
//     live caller left to run it, so a separate goroutine waits for
//     the abandoned write to resolve. select picks pseudo-randomly
//     when both cases are ready at once, so the write may in fact have
//     already delivered q to the peer even though this branch ran;
//     continueAfterAbandonedWrite handles both outcomes and is the
//     only place that calls release for this path.
//   - If the write finishes first and fails, this call is terminal
//     too, so release runs immediately, before writeQuestion returns.
//   - If the write finishes first and succeeds, release does not run
//     here: the caller hands the still-held lock to readAnswer, which
//     releases it once that phase resolves.
func writeQuestion(ctx context.Context, r io.Reader, w io.Writer, q Question, release func()) error {
	done := make(chan error, 1)
	go func() {
		done <- encodeQuestionLine(w, q)
	}()
	select {
	case <-ctx.Done():
		go continueAfterAbandonedWrite(done, r, q.ID, release)
		return ctx.Err()
	case err := <-done:
		if err != nil {
			release()
		}
		return err
	}
}

// continueAfterAbandonedWrite waits for a ctx-abandoned write to
// resolve, then decides whether the call is terminal. A write failure
// is terminal: release the lock, matching the synchronous-failure
// path. A write success means q reached the peer, so the call is not
// terminal; a read phase is still owed. The original call already
// returned ctx.Err() to its caller, so there is no live caller left to
// invoke readAnswer: this goroutine runs the same scanAnswerLine step
// itself and releases the lock once that resolves, matching
// readAnswer's own abandoned-operation model and keeping a second
// caller locked out until both phases of the abandoned call finish.
func continueAfterAbandonedWrite(done <-chan error, r io.Reader, wantID string, release func()) {
	if err := <-done; err != nil {
		release()
		return
	}
	scanAnswerLine(r, wantID)
	release()
}

// encodeQuestionLine marshals q into the ndjsonQuestionLine wire form
// and writes it to w as one JSON-encoded line, through
// json.NewEncoder(w).Encode, matching mivia-agent's hub.connection.go
// writeLoop call shape.
func encodeQuestionLine(w io.Writer, q Question) error {
	line := ndjsonQuestionLine{
		Type:      ndjsonQuestionType,
		ID:        q.ID,
		Recipient: q.Recipient,
		Payload:   q.Payload,
	}
	if err := jsonEncode(w, line); err != nil {
		return fmt.Errorf("channel: ndjson: %w", err)
	}
	return nil
}

// readAnswer blocks reading one NDJSON line from r, decoding it into
// an Answer that must carry "type":"answer" and a question_id equal
// to wantID. The blocking bufio.Scanner call runs in a goroutine so
// readAnswer can also select on ctx.Done(); release, always non-nil,
// is called from inside that goroutine on its own exit (a line
// arrives, or r errors or closes), never from the ctx.Done() branch,
// so a second call never starts its own Scan on r while this one is
// still pending. A ctx cancellation returns ctx.Err() to this call's
// caller at once without waiting for release.
func readAnswer(ctx context.Context, r io.Reader, wantID string, release func()) (Answer, error) {
	done := make(chan ndjsonReadResult, 1)
	go func() {
		// release runs before the send on done, not deferred after
		// it: a caller that receives a result must never observe the
		// lock as still held, or an immediate next call on the same
		// closure would see a spurious ErrNotifierBusy even though
		// this call already finished.
		res := scanAnswerLine(r, wantID)
		release()
		done <- res
	}()
	select {
	case <-ctx.Done():
		return Answer{}, ctx.Err()
	case res := <-done:
		return res.ans, res.err
	}
}

// ndjsonReadResult carries readAnswer's decoded outcome across its
// background goroutine.
type ndjsonReadResult struct {
	ans Answer
	err error
}

// scanAnswerLine blocks reading and decoding one NDJSON answer line
// from r, checking it against wantID. It never touches the lock; the
// caller releases it once this call returns.
func scanAnswerLine(r io.Reader, wantID string) ndjsonReadResult {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, ndjsonScannerInitialBuffer), ndjsonScannerMaxBuffer)
	if !sc.Scan() {
		err := sc.Err()
		if err == nil {
			err = io.EOF
		}
		return ndjsonReadResult{err: fmt.Errorf("channel: ndjson: %w", err)}
	}
	var line ndjsonAnswerLine
	if err := jsonDecode(sc.Bytes(), &line); err != nil {
		return ndjsonReadResult{err: fmt.Errorf("channel: ndjson: %w", err)}
	}
	if line.Type != ndjsonAnswerType {
		return ndjsonReadResult{err: fmt.Errorf("channel: ndjson: unexpected line type %q", line.Type)}
	}
	if line.QuestionID != wantID {
		return ndjsonReadResult{err: ErrAnswerMismatch}
	}
	return ndjsonReadResult{ans: Answer{
		QuestionID: line.QuestionID,
		Approved:   line.Approved,
		Payload:    line.Payload,
	}}
}
