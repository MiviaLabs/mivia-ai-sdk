package spool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// MoreMarker ends a page with more bytes after it, naming the next
// offset.
const MoreMarker = "[more: offset=%d]"

// readOutputName is the tool's registry name.
const readOutputName = "read_spooled_output"

// readOutputSchema is the tool's published parameter schema: a
// required ref, an optional non-negative offset, and an optional
// non-negative limit.
const readOutputSchema = `{"type":"object","properties":{` +
	`"ref":{"type":"string"},` +
	`"offset":{"type":"integer","minimum":0},` +
	`"limit":{"type":"integer","minimum":0}},` +
	`"required":["ref"]}`

// readOutputArgs is the decoded argument shape of ReadOutputTool.
type readOutputArgs struct {
	Ref    string `json:"ref"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// readOutputTool is the model-facing read-back tool over one Spool.
type readOutputTool struct {
	sp           *Spool
	maxPageBytes int
}

// ReadOutputTool builds the model-facing read-back tool over sp. The
// model pages a spooled body back by ref, offset, and limit. A nil sp
// wraps ErrNilSpool; a non-positive maxPageBytes wraps ErrInvalidLimit.
func ReadOutputTool(sp *Spool, maxPageBytes int) (tools.Tool, error) {
	if sp == nil {
		return nil, fmt.Errorf("%w", ErrNilSpool)
	}
	if maxPageBytes <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidLimit, maxPageBytes)
	}
	return &readOutputTool{sp: sp, maxPageBytes: maxPageBytes}, nil
}

// Name returns the tool's registry name.
func (t *readOutputTool) Name() string { return readOutputName }

// ParameterSchema publishes the ref, offset, and limit schema.
func (t *readOutputTool) ParameterSchema() []byte { return []byte(readOutputSchema) }

// DecodeArguments decodes raw into the tool's own argument shape. A
// malformed payload wraps ErrBadArguments.
func (t *readOutputTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	var args readOutputArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.InOut{}, fmt.Errorf("%w: %v", ErrBadArguments, err)
	}
	if args.Offset < 0 || args.Limit < 0 {
		return tools.InOut{}, fmt.Errorf("%w: negative offset or limit", ErrBadArguments)
	}
	return tools.InOut{Value: args}, nil
}

// Run pages the spooled body named by in's ref back under the ctx
// principal. A limit of zero means one full page of maxPageBytes
// bytes; a limit over maxPageBytes clamps to it. The page carries
// MoreMarker with the next offset when bytes remain, and nothing
// extra on the final page. An offset past the end returns an empty
// page with no marker.
func (t *readOutputTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(readOutputArgs)
	if !ok {
		return tools.Out{}, fmt.Errorf("%w: wrong input type %T", ErrBadArguments, in.Value)
	}
	if args.Offset < 0 || args.Limit < 0 {
		return tools.Out{}, fmt.Errorf("%w: negative offset or limit", ErrBadArguments)
	}
	principal, ok := PrincipalFrom(ctx)
	if !ok {
		return tools.Out{}, fmt.Errorf("%w", ErrNoPrincipal)
	}
	data, err := t.sp.Load(ctx, principal, args.Ref)
	if err != nil {
		return tools.Out{}, err
	}
	if args.Offset >= len(data) {
		return tools.Out{Value: ""}, nil
	}
	limit := args.Limit
	if limit == 0 || limit > t.maxPageBytes {
		limit = t.maxPageBytes
	}
	end := args.Offset + limit
	if end > len(data) {
		end = len(data)
	}
	page := string(data[args.Offset:end])
	if end < len(data) {
		return tools.Out{Value: page + fmt.Sprintf(MoreMarker, end)}, nil
	}
	return tools.Out{Value: page}, nil
}
