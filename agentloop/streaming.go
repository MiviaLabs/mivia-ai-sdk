package agentloop

import (
	"bytes"
	"io"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// newStreamBuffer returns the per-run capture buffer when the caller
// set Options.StreamingWriter, nil otherwise. run creates it once per
// Run or RunSteerable call and threads it down, so concurrent runs on
// one Loop never share buffer state.
func newStreamBuffer(sink io.Writer) *bytes.Buffer {
	if sink == nil {
		return nil
	}
	return &bytes.Buffer{}
}

// streamMirror returns the writer the Completer writes to for one
// runChat call: the caller's Options.StreamingWriter and the capture
// buffer together, so the caller sees the same bytes the loop
// buffers. It resets the buffer first: a prior canceled iteration's
// partial must not bleed into this call's Final.
func streamMirror(sink io.Writer, buf *bytes.Buffer) io.Writer {
	if buf == nil {
		return nil
	}
	buf.Reset()
	return io.MultiWriter(sink, buf)
}

// steeredStopResult builds the Result for the non-injector
// Steered-stop path. The capture buffer becomes Final.Content when
// populated, so a partial reply survives the cancel.
func steeredStopResult(history []provider.Message, iterations int, totalUsage provider.Usage, buf *bytes.Buffer) Result {
	res := Result{History: history, Iterations: iterations, Usage: totalUsage, Stop: StopSteered}
	if buf != nil && buf.Len() > 0 {
		res.Final = provider.Message{
			Role:    provider.RoleAssistant,
			Content: buf.String(),
		}
	}
	return res
}
