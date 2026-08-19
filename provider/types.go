package provider

import (
	"errors"
	"unicode/utf8"
)

// Sentinel errors for Message.Validate and Chunk.Validate; test with
// errors.Is.
var (
	// ErrToolCallIDUnexpected is Validate's error when ToolCallID is
	// non-empty on a Message whose Role is not RoleTool.
	ErrToolCallIDUnexpected = errors.New("provider: tool call id unexpected outside RoleTool")
	// ErrToolCallIDRequired is Validate's error when ToolCallID is
	// empty on a RoleTool Message.
	ErrToolCallIDRequired = errors.New("provider: tool call id required for RoleTool")
	// ErrUnknownRole is Validate's error when Role is outside the four
	// declared constants.
	ErrUnknownRole = errors.New("provider: unknown role")
	// ErrToolCallsUnexpected is Validate's error when ToolCalls is
	// non-empty on a Message whose Role is not RoleAssistant.
	ErrToolCallsUnexpected = errors.New("provider: tool calls unexpected outside RoleAssistant")
	// ErrChunkErrDoneConflict is Chunk.Validate's error when a Chunk
	// carries both a non-nil Err and Done == true.
	ErrChunkErrDoneConflict = errors.New("provider: chunk carries both Err and Done")
	// ErrStreamClosedEarly is drainStream's error when a Completer's
	// ChatStream channel closes before any chunk carries Done == true
	// or a non-nil Err. RunTurn returns the zero Response alongside
	// this error; it never returns a partial aggregation.
	ErrStreamClosedEarly = errors.New("provider: stream closed before a terminal chunk")
	// ErrNameUnexpected is Validate's error when Name is non-empty on a
	// Role other than RoleUser or RoleTool.
	ErrNameUnexpected = errors.New("provider: name unexpected outside RoleUser and RoleTool")
	// ErrNameInvalid is Validate's error when a non-empty Name exceeds
	// MaxNameBytes, is not valid UTF-8, or carries a control character.
	ErrNameInvalid = errors.New("provider: name is invalid or too long")
	// ErrPromptTooLong marks a provider's rejection of a prompt that
	// exceeds the model's context window. A Completer returns or wraps
	// it; provider ships no implementation itself.
	ErrPromptTooLong = errors.New("provider: prompt exceeds the model context window")
)

// MaxNameBytes bounds Message.Name when set.
const MaxNameBytes = 128

// Role names a message's role in a chat turn.
type Role string

// The four roles a Message may carry.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one turn in the conversation Request.Messages carries.
// ToolCallID is set only, and always, on a RoleTool message; it names
// the ToolCall.ID the message answers. ToolCalls is non-empty only on
// a RoleAssistant message; it holds the calls that assistant turn made.
// Name is legal only on RoleUser and RoleTool messages; an empty Name
// is legal on every role. See MaxNameBytes for the bound.
type Message struct {
	Role       Role
	Content    string
	Name       string
	ToolCallID string
	ToolCalls  []ToolCall
}

// Validate enforces the ToolCallID/Role pairing rule, the closed set
// of Role constants, the Name rule, and the ToolCalls rule. It checks
// Role legality first: a Role outside the four constants always
// returns ErrUnknownRole, regardless of Name, ToolCallID, or
// ToolCalls. For one of the four known roles, Validate next checks
// the Name rule: ErrNameUnexpected when Name is non-empty on a Role
// other than RoleUser or RoleTool; ErrNameInvalid when a non-empty
// Name exceeds MaxNameBytes, is not valid UTF-8, or carries a control
// character. Only then does Validate check the ToolCallID pairing
// rule: ErrToolCallIDUnexpected when ToolCallID is non-empty on a
// non-RoleTool message; ErrToolCallIDRequired when ToolCallID is empty
// on a RoleTool message. Finally, Validate rejects a non-empty
// ToolCalls on any known Role other than RoleAssistant with
// ErrToolCallsUnexpected. RunTurn calls Validate on every entry of
// Request.Messages before it dispatches.
func (m Message) Validate() error {
	switch m.Role {
	case RoleSystem, RoleUser:
		if err := m.validateName(); err != nil {
			return err
		}
		if m.ToolCallID != "" {
			return ErrToolCallIDUnexpected
		}
		if len(m.ToolCalls) > 0 {
			return ErrToolCallsUnexpected
		}
	case RoleAssistant:
		if err := m.validateName(); err != nil {
			return err
		}
		if m.ToolCallID != "" {
			return ErrToolCallIDUnexpected
		}
	case RoleTool:
		if err := m.validateName(); err != nil {
			return err
		}
		if m.ToolCallID == "" {
			return ErrToolCallIDRequired
		}
		if len(m.ToolCalls) > 0 {
			return ErrToolCallsUnexpected
		}
	default:
		return ErrUnknownRole
	}
	return nil
}

// validateName applies the Name rule for one known role. A non-empty
// Name outside RoleUser and RoleTool is ErrNameUnexpected; a malformed
// Name on any role is ErrNameInvalid.
func (m Message) validateName() error {
	if m.Name == "" {
		return nil
	}
	if m.Role != RoleUser && m.Role != RoleTool {
		return ErrNameUnexpected
	}
	if len(m.Name) > MaxNameBytes || !utf8.ValidString(m.Name) {
		return ErrNameInvalid
	}
	for _, r := range m.Name {
		if r < 0x20 || r == 0x7f {
			return ErrNameInvalid
		}
	}
	return nil
}

// ToolDefinition names one tool a model may call. Schema holds the
// tool's parameter schema as raw bytes; provider does not parse it.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      []byte
}

// ToolCall is one call the model requests, or one fragment of a call
// while it streams. Index is the vendor-assigned position of this
// tool call within the turn. Arguments holds the raw argument bytes;
// the caller decodes them against the matching ToolDefinition.Schema.
type ToolCall struct {
	Index     int
	ID        string
	Name      string
	Arguments []byte
}

// Usage reports token accounting for one completed turn. CachedTokens
// counts prompt tokens served from a provider-side cache, when the
// provider reports one; it is zero otherwise.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
}

// Request is the input to every Completer method. An empty Model
// means the implementation's own default. Tools may be empty when the
// caller offers none.
type Request struct {
	Model    string
	Messages []Message
	Tools    []ToolDefinition
	Stream   bool
}

// Response is the aggregated result of one turn. Model echoes the
// model that actually served the request, which may differ from
// Request.Model on a provider that redirects to a fallback. ToolCalls
// is empty when the model returned plain text.
type Response struct {
	Model        string
	Message      Message
	ToolCalls    []ToolCall
	Usage        Usage
	FinishReason string
}

// Chunk is one increment of a streamed response. Done is true only on
// the final chunk that completes without error; Usage and
// FinishReason are the zero value until then. ToolCallDelta is
// non-nil only on a chunk that carries a tool-call fragment. Err is
// nil on every chunk except a terminal chunk that reports a
// mid-stream failure; when a chunk carries a non-nil Err, the channel
// closes after it and no further chunk follows. A chunk never carries
// both a non-nil Err and Done == true.
type Chunk struct {
	Delta         string
	ToolCallDelta *ToolCall
	Done          bool
	Usage         Usage
	FinishReason  string
	Err           error
}

// Validate enforces that Err and Done == true are mutually exclusive
// on one Chunk, returning ErrChunkErrDoneConflict when they are not.
// RunTurn's drain loop calls Validate on every Chunk it reads before
// it applies the chunk's Err or Done value.
func (c Chunk) Validate() error {
	if c.Err != nil && c.Done {
		return ErrChunkErrDoneConflict
	}
	return nil
}
