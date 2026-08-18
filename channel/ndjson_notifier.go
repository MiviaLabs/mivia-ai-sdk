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
// stdio and hub protocols already use.
//
// The returned Notifier serves one call at a time: a second call
// while one is already in flight, including one whose ctx was
// already canceled but whose background read has not yet resolved,
// returns ErrNotifierBusy immediately, without touching r or w. A
// caller that wires this closure into more than one concurrent call
// site (for example two independent agent.Run calls sharing one
// stdio pipe) must serialize its own calls or expect ErrNotifierBusy
// on overlap.
//
// After a ctx-canceled call, the closure stays locked until the
// abandoned read resolves: a line arrives, or the peer closes or
// errors r. Until then, every subsequent call on that closure returns
// ErrNotifierBusy. This is a deliberate limit, not an oversight: it
// is the price of never letting a second call start its own Scan on r
// while a first call's background Scan is still pending, which would
// corrupt the shared NDJSON stream. Closing r makes the stale read
// return an error, releasing the lock; the closure is usable again
// after that.
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
		if err := writeQuestion(w, q); err != nil {
			mu.Unlock()
			return Answer{}, fmt.Errorf("channel: ndjson: %w", err)
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

// writeQuestion marshals q into the ndjsonQuestionLine wire form and
// writes it to w as one JSON-encoded line, through
// json.NewEncoder(w).Encode, matching mivia-agent's hub.connection.go
// writeLoop call shape.
func writeQuestion(w io.Writer, q Question) error {
	line := ndjsonQuestionLine{
		Type:      ndjsonQuestionType,
		ID:        q.ID,
		Recipient: q.Recipient,
		Payload:   q.Payload,
	}
	return jsonEncode(w, line)
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
