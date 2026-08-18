package dispatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// SendResult is one reply line's outcome, in request order.
type SendResult struct {
	Ack envelope.Ack
	Err error // set when the server answered an error line
}

// Send posts msgs as one NDJSON request to url and returns one
// SendResult per reply line, in the order the server answered. An
// error line from the server surfaces as that entry's Err; a decode
// failure on a reply line surfaces the same way.
func Send(ctx context.Context, url string, msgs []envelope.Message) ([]SendResult, error) {
	body, err := encodeRequest(msgs)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseReply(reply), nil
}

// encodeRequest builds the NDJSON request body from msgs.
func encodeRequest(msgs []envelope.Message) ([]byte, error) {
	var buf bytes.Buffer
	for _, m := range msgs {
		data, err := m.Encode()
		if err != nil {
			return nil, fmt.Errorf("dispatch: encode message %s: %w", m.ID, err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// parseReply splits body into lines and decodes each into a
// SendResult, in order. A blank line is skipped.
func parseReply(body []byte) []SendResult {
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	results := make([]SendResult, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if msg, ok := decodeErrorLine(line); ok {
			results = append(results, SendResult{Err: errors.New(msg)})
			continue
		}
		ack, err := envelope.DecodeAck(line)
		if err != nil {
			results = append(results, SendResult{Err: err})
			continue
		}
		results = append(results, SendResult{Ack: ack})
	}
	return results
}
