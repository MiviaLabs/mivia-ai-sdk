package trace

import (
	"encoding/json"
	"io"
	"time"
)

// jsonSpan is the on-wire shape WriteJSONLines encodes one Span into.
type jsonSpan struct {
	Name       string            `json:"name"`
	Start      time.Time         `json:"start"`
	End        time.Time         `json:"end"`
	Parent     SpanID            `json:"parent"`
	Attributes map[string]string `json:"attributes"`
}

// WriteJSONLines writes one JSON object per span to w, one span per
// line, in the slice's order. Each line carries name, start, end,
// parent, and attributes. End is the zero time.Time for a span whose
// End method has not run. An empty spans slice writes nothing.
func WriteJSONLines(w io.Writer, spans []*Span) error {
	enc := json.NewEncoder(w)
	for _, s := range spans {
		rec := jsonSpan{
			Name:       s.Name,
			Start:      s.Start,
			End:        s.EndTime(),
			Parent:     s.ParentID,
			Attributes: s.Attributes(),
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}
