package tools

// SchemaTool is an optional interface. A Tool implements it to
// publish its parameter schema and decode raw argument bytes.
type SchemaTool interface {
	// ParameterSchema returns the tool's parameter schema as raw
	// bytes, in a format the caller's model provider understands.
	ParameterSchema() []byte
	// DecodeArguments turns raw model-supplied argument bytes into
	// the tool's own InOut input value.
	DecodeArguments(raw []byte) (InOut, error)
}

// SchemaOf returns t.ParameterSchema() and true when t implements
// SchemaTool; else it returns nil, false. It follows
// ExecutionProfileOf's precedent: an optional marker, checked through
// a type assertion, with a paired accessor.
func SchemaOf(t Tool) ([]byte, bool) {
	if st, ok := t.(SchemaTool); ok {
		return st.ParameterSchema(), true
	}
	return nil, false
}
