package channel

import (
	"encoding/json"
	"io"
)

// jsonEncode writes v to w as one JSON-encoded line, through
// json.NewEncoder(w).Encode, the same call shape mivia-agent's
// hub.connection.go writeLoop uses. Named in this file, matching this
// module's convention (see envelope/message.go, machine/wire.go,
// flow/wire.go, ledger/wire.go) that wire-bytes marshaling stays in a
// file named message.go, sign.go, ack.go, or wire.go.
func jsonEncode(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// jsonDecode unmarshals one scanned NDJSON line into v.
func jsonDecode(line []byte, v any) error {
	return json.Unmarshal(line, v)
}
