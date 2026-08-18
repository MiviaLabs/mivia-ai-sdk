package dispatch

import "encoding/json"

// errorLine is the one JSON shape a failed ladder stage emits.
type errorLine struct {
	Error string `json:"error"`
}

// encodeErrorLine builds the wire form of a stage failure. Marshal of
// a single-string-field struct cannot fail, so the error is ignored.
func encodeErrorLine(err error) []byte {
	data, _ := json.Marshal(errorLine{Error: err.Error()})
	return data
}

// decodeErrorLine reports whether data is an error line and, if so,
// its message. A non-error line (an ack) leaves ok false.
func decodeErrorLine(data []byte) (msg string, ok bool) {
	var e errorLine
	if err := json.Unmarshal(data, &e); err != nil {
		return "", false
	}
	if e.Error == "" {
		return "", false
	}
	return e.Error, true
}
