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
// SchemaTool and publishes non-nil schema bytes. It fails closed: a
// tool whose ParameterSchema() returns nil gets nil, false, whether
// or not it implements SchemaTool, so a nil schema never reads as
// published. It follows ExecutionProfileOf's precedent: an optional
// marker, checked through a type assertion, with a paired accessor.
func SchemaOf(t Tool) ([]byte, bool) {
	if st, ok := t.(SchemaTool); ok {
		schema := st.ParameterSchema()
		if schema == nil {
			return nil, false
		}
		return schema, true
	}
	return nil, false
}
