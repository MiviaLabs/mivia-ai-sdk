// FileTools is the one owning constructor the five file tools accept
// a Workspace from. See docs/plans/subagent.md's "File tools
// addendum" for the full design.

package subagent

import (
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/secretpath"
	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// ErrDenyRequired reports a FileToolOptions with a nil Deny.
// FileToolOptions.Validate returns it before OpenFileTools opens any
// file: the five file tools never accept a workspace with no secret
// policy, even an empty one (secretpath.NewMatcher(nil)).
var ErrDenyRequired = errors.New("subagent: FileToolOptions.Deny is required")

// FileToolOptions configures OpenFileTools. Root and MaxReadBytes
// pass through to workspace.Options unchanged; Deny is mandatory
// here even though workspace.Options.Deny is optional, because
// subagent's file tools are the one place in this module that hands
// filesystem access to a model.
type FileToolOptions struct {
	// Root is the directory the opened Workspace confines access to.
	Root string
	// Deny refuses a path it matches, and refuses any path holding a
	// symlink component. Required: Validate rejects a nil Deny.
	Deny *secretpath.Matcher
	// MaxReadBytes bounds one read. Zero selects
	// workspace.DefaultMaxReadBytes; workspace.Unbounded removes the
	// bound. See workspace.Options.Validate.
	MaxReadBytes int64
}

// Validate reports whether o names a usable FileTools. Deny must not
// be nil; Root and MaxReadBytes follow workspace.Options.Validate's
// rules, checked by workspace.OpenWith itself.
func (o FileToolOptions) Validate() error {
	if o.Deny == nil {
		return ErrDenyRequired
	}
	return nil
}

// FileTools bundles one opened *workspace.Workspace so the five file
// tools share one open root and one Close owner. Safe for concurrent
// use by multiple tools' Run calls: os.Root's methods are documented
// safe for concurrent use, and FileTools adds no further mutable
// state.
type FileTools struct {
	ws *workspace.Workspace
}

// OpenFileTools validates opts, then opens a Workspace on opts.Root
// under opts.MaxReadBytes and opts.Deny. It returns ErrDenyRequired
// unchanged when opts.Deny is nil, and workspace.OpenWith's error
// unchanged for any other invalid or unopenable root. The caller
// owns the returned value's Close.
func OpenFileTools(opts FileToolOptions) (*FileTools, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	ws, err := workspace.OpenWith(workspace.Options{
		Root:         opts.Root,
		MaxReadBytes: opts.MaxReadBytes,
		Deny:         opts.Deny,
	})
	if err != nil {
		return nil, err
	}
	return &FileTools{ws: ws}, nil
}

// Close closes the Workspace OpenFileTools opened. Close is
// idempotent, matching workspace.Workspace.Close.
func (f *FileTools) Close() error {
	return f.ws.Close()
}
