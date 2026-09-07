// ReadOutputTool tests: construction bounds, paging, argument and
// principal handling, and the published schema.
package memory_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// newReadFixture builds one Spool holding body under alice and the
// read-back tool over it, with a page budget of page bytes.
func newReadFixture(t *testing.T, body string, page int) (tools.Tool, *memory.Spool, string) {
	t.Helper()
	store := newFakeStore()
	sp, err := memory.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	_, ref, err := sp.Spool(context.Background(), "alice", []byte(body))
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	tool, err := memory.ReadOutputTool(sp, page)
	if err != nil {
		t.Fatalf("ReadOutputTool: %v", err)
	}
	return tool, sp, ref
}

// runRead decodes raw and runs the tool under principal.
func runRead(t *testing.T, tool tools.Tool, principal, raw string) (tools.Out, error) {
	t.Helper()
	in, err := tool.(tools.SchemaTool).DecodeArguments([]byte(raw))
	if err != nil {
		return tools.Out{}, err
	}
	ctx := context.Background()
	if principal != "" {
		ctx = memory.WithPrincipal(ctx, principal)
	}
	return tool.Run(ctx, in)
}

func TestReadOutputToolConstruction(t *testing.T) {
	store := newFakeStore()
	sp, _ := memory.NewSpool(store, 128)
	if _, err := memory.ReadOutputTool(nil, 128); !errors.Is(err, memory.ErrNilSpool) {
		t.Fatalf("ReadOutputTool(nil, _) = %v, want errors.Is ErrNilSpool", err)
	}
	for _, page := range []int{0, -1} {
		if _, err := memory.ReadOutputTool(sp, page); !errors.Is(err, memory.ErrInvalidLimit) {
			t.Fatalf("ReadOutputTool(sp, %d) = %v, want errors.Is ErrInvalidLimit", page, err)
		}
	}
}

func TestReadOutputToolPaging(t *testing.T) {
	body := strings.Repeat("012345", 1) + strings.Repeat("6789ab", 1) + strings.Repeat("cdef01", 1)
	tool, _, ref := newReadFixture(t, body, 6)
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "first page carries the next offset",
			raw:  fmt.Sprintf(`{"ref":%q}`, ref),
			want: "012345" + fmt.Sprintf(memory.MoreMarker, 6),
		},
		{
			name: "middle page carries the next offset",
			raw:  fmt.Sprintf(`{"ref":%q,"offset":6}`, ref),
			want: "6789ab" + fmt.Sprintf(memory.MoreMarker, 12),
		},
		{
			name: "final page carries nothing extra",
			raw:  fmt.Sprintf(`{"ref":%q,"offset":12}`, ref),
			want: "cdef01",
		},
		{
			name: "offset past the end yields an empty page",
			raw:  fmt.Sprintf(`{"ref":%q,"offset":999}`, ref),
			want: "",
		},
		{
			name: "limit over the page budget clamps",
			raw:  fmt.Sprintf(`{"ref":%q,"offset":0,"limit":4096}`, ref),
			want: "012345" + fmt.Sprintf(memory.MoreMarker, 6),
		},
		{
			name: "a small limit bounds the page",
			raw:  fmt.Sprintf(`{"ref":%q,"offset":0,"limit":3}`, ref),
			want: "012" + fmt.Sprintf(memory.MoreMarker, 3),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := runRead(t, tool, "alice", c.raw)
			if err != nil {
				t.Fatalf("Run = %v, want nil", err)
			}
			if out.Value.(string) != c.want {
				t.Fatalf("page = %q, want %q", out.Value, c.want)
			}
		})
	}
}

func TestReadOutputToolBadArguments(t *testing.T) {
	tool, _, ref := newReadFixture(t, "body", 4)
	_, err := tool.(tools.SchemaTool).DecodeArguments([]byte(`{"ref":`))
	if !errors.Is(err, memory.ErrBadArguments) {
		t.Fatalf("DecodeArguments on malformed JSON = %v, want errors.Is ErrBadArguments", err)
	}
	for _, raw := range []string{
		fmt.Sprintf(`{"ref":%q,"offset":-1}`, ref),
		fmt.Sprintf(`{"ref":%q,"limit":-1}`, ref),
	} {
		if _, err := tool.(tools.SchemaTool).DecodeArguments([]byte(raw)); !errors.Is(err, memory.ErrBadArguments) {
			t.Fatalf("DecodeArguments(%s) = %v, want errors.Is ErrBadArguments", raw, err)
		}
	}
	in := tools.InOut{Value: "not the argument type"}
	_, err = tool.Run(memory.WithPrincipal(context.Background(), "alice"), in)
	if !errors.Is(err, memory.ErrBadArguments) {
		t.Fatalf("Run with a mistyped input = %v, want errors.Is ErrBadArguments", err)
	}
}

func TestReadOutputToolPrincipalAndRefErrors(t *testing.T) {
	tool, sp, ref := newReadFixture(t, "body", 4)
	if _, err := runRead(t, tool, "alice", fmt.Sprintf(`{"ref":%q}`, ref)); err != nil {
		t.Fatalf("Run under the granting principal = %v, want nil", err)
	}
	_, err := runRead(t, tool, "bob", fmt.Sprintf(`{"ref":%q}`, ref))
	if !errors.Is(err, memory.ErrWrongPrincipal) {
		t.Fatalf("Run under a wrong principal = %v, want errors.Is ErrWrongPrincipal", err)
	}
	_, err = runRead(t, tool, "", fmt.Sprintf(`{"ref":%q}`, ref))
	if !errors.Is(err, memory.ErrNoPrincipal) {
		t.Fatalf("Run with no principal = %v, want errors.Is ErrNoPrincipal", err)
	}
	_, err = runRead(t, tool, "alice", `{"ref":"ref-missing"}`)
	if !errors.Is(err, memory.ErrUnknownGrantRef) {
		t.Fatalf("Run on an unknown ref = %v, want errors.Is ErrUnknownGrantRef", err)
	}
	if err := sp.Expire(ref); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	_, err = runRead(t, tool, "alice", fmt.Sprintf(`{"ref":%q}`, ref))
	if !errors.Is(err, memory.ErrExpired) {
		t.Fatalf("Run on an expired grant = %v, want errors.Is ErrExpired", err)
	}
}

func TestReadOutputToolPublishesSchema(t *testing.T) {
	tool, _, _ := newReadFixture(t, "body", 4)
	schema, ok := tools.SchemaOf(tool)
	if !ok {
		t.Fatal("tools.SchemaOf reports no schema, want the tool to be offerable")
	}
	if len(schema) == 0 {
		t.Fatal("published schema is empty")
	}
	in, err := tool.(tools.SchemaTool).DecodeArguments([]byte(`{"ref":"r","offset":3,"limit":2}`))
	if err != nil {
		t.Fatalf("DecodeArguments: %v", err)
	}
	out, err := tool.Run(memory.WithPrincipal(context.Background(), "alice"), in)
	if err == nil {
		_ = out
	}
	if tool.Name() == "" {
		t.Fatal("tool name is empty")
	}
}

func TestReadOutputToolConcurrent(t *testing.T) {
	body := strings.Repeat("abcdef", 64)
	tool, sp, ref := newReadFixture(t, body, 32)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				raw := fmt.Sprintf(`{"ref":%q,"offset":%d}`, ref, offset)
				out, err := runReadConcurrent(tool, "alice", raw)
				if err != nil {
					// Expiry races the pages; every failure must
					// still be a typed sentinel.
					if !errors.Is(err, memory.ErrExpired) && !errors.Is(err, memory.ErrUnknownGrantRef) {
						t.Errorf("Run: %v", err)
					}
					continue
				}
				page := out.Value.(string)
				if len(page) > 32+len(fmt.Sprintf(memory.MoreMarker, 1<<20)) {
					t.Errorf("torn page of %d bytes", len(page))
					return
				}
			}
		}(g * 16)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_ = sp.Expire(ref)
		}
	}()
	wg.Wait()
}

// runReadConcurrent mirrors runRead without t.Helper, safe for
// goroutines.
func runReadConcurrent(tool tools.Tool, principal, raw string) (tools.Out, error) {
	in, err := tool.(tools.SchemaTool).DecodeArguments([]byte(raw))
	if err != nil {
		return tools.Out{}, err
	}
	ctx := memory.WithPrincipal(context.Background(), principal)
	return tool.Run(ctx, in)
}
