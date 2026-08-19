package contextstate_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// fixtureRef mints a valid ContentRef over data.
func fixtureRef(t *testing.T, data []byte) contextstate.ContentRef {
	t.Helper()
	ref, err := contextstate.NewContentRef("fixture-ns", "workspace-a", "session-a", "subject-a", data)
	if err != nil {
		t.Fatalf("NewContentRef: %v", err)
	}
	return ref
}

func TestPayloadRecordValidate(t *testing.T) {
	data := []byte("payload-bytes")
	valid := func(retention contextstate.RetentionClass) contextstate.PayloadRecord {
		return contextstate.PayloadRecord{Ref: fixtureRef(t, data), Retention: retention, Data: data}
	}
	accept := []struct {
		name      string
		retention contextstate.RetentionClass
	}{
		{"session class", contextstate.RetentionSession},
		{"compliance class", contextstate.RetentionCompliance},
		{"caller-defined class", "team-defined"},
	}
	for _, tc := range accept {
		t.Run(tc.name, func(t *testing.T) {
			if err := valid(tc.retention).Validate(); err != nil {
				t.Fatalf("Validate rejected a valid record: %v", err)
			}
		})
	}
	t.Run("absent data accepted", func(t *testing.T) {
		refOnly := contextstate.PayloadRecord{Ref: fixtureRef(t, data), Retention: contextstate.RetentionSession}
		if err := refOnly.Validate(); err != nil {
			t.Fatalf("Validate rejected a record without data: %v", err)
		}
	})
	sizeMismatch := valid(contextstate.RetentionSession)
	sizeMismatch.Ref.Size = len(data) + 1
	digestMismatch := valid(contextstate.RetentionSession)
	digestMismatch.Data = append([]byte(nil), data...)
	digestMismatch.Data[0] ^= 1
	invalidRef := valid(contextstate.RetentionSession)
	invalidRef.Ref.Ref = "not-a-ref"
	reject := []struct {
		name   string
		record contextstate.PayloadRecord
	}{
		{"invalid ref", invalidRef},
		{"empty retention", contextstate.PayloadRecord{Ref: fixtureRef(t, data), Data: data}},
		{"size mismatch", sizeMismatch},
		{"digest mismatch", digestMismatch},
	}
	for _, tc := range reject {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.record.Validate()
			if err == nil {
				t.Fatal("Validate accepted an invalid record")
			}
			if !errors.Is(err, contextstate.ErrInvalidRecord) {
				t.Fatalf("error %v does not wrap ErrInvalidRecord", err)
			}
		})
	}
}

func TestContentRefValidate(t *testing.T) {
	base := func() contextstate.ContentRef { return fixtureRef(t, []byte("ref-bytes")) }
	otherDigest := contextstate.Digest([]byte("other-bytes"))
	cases := []struct {
		name    string
		mutate  func(*contextstate.ContentRef)
		wantErr bool
	}{
		{"valid", func(*contextstate.ContentRef) {}, false},
		{"non-canonical ref", func(r *contextstate.ContentRef) { r.Ref = "not-a-ref" }, true},
		{"ref off the bare digest", func(r *contextstate.ContentRef) {
			r.Ref = contextstate.HashPrefix + otherDigest
		}, true},
		{"blank namespace", func(r *contextstate.ContentRef) { r.Namespace = "" }, true},
		{"blank workspace id", func(r *contextstate.ContentRef) { r.WorkspaceID = "" }, true},
		{"blank session id", func(r *contextstate.ContentRef) { r.SessionID = "" }, true},
		{"blank subject id", func(r *contextstate.ContentRef) { r.SubjectID = "" }, true},
		{"workspace id over max", func(r *contextstate.ContentRef) { r.WorkspaceID = id129 }, true},
		{"session id over max", func(r *contextstate.ContentRef) { r.SessionID = id129 }, true},
		{"subject id over max", func(r *contextstate.ContentRef) { r.SubjectID = id129 }, true},
		{"negative size", func(r *contextstate.ContentRef) { r.Size = -1 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := base()
			tc.mutate(&ref)
			err := ref.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid ContentRef")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid ContentRef: %v", err)
			}
		})
	}
}

func TestValidationErrorFormatting(t *testing.T) {
	var nilErr *contextstate.ValidationError
	if got := nilErr.Error(); got != contextstate.ErrInvalidRecord.Error() {
		t.Fatalf("nil Error() = %q, want %q", got, contextstate.ErrInvalidRecord.Error())
	}
	emptyField := &contextstate.ValidationError{Reason: "broken"}
	if got, want := emptyField.Error(), contextstate.ErrInvalidRecord.Error()+": broken"; got != want {
		t.Fatalf("empty-field Error() = %q, want %q", got, want)
	}
	full := &contextstate.ValidationError{Field: "content.size", Reason: "negative"}
	if got, want := full.Error(), contextstate.ErrInvalidRecord.Error()+": content.size: negative"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(full, contextstate.ErrInvalidRecord) {
		t.Fatal("ValidationError does not unwrap into ErrInvalidRecord")
	}
}

func TestReassemble(t *testing.T) {
	whole := []byte("abcdef")
	ref := fixtureRef(t, whole)
	t.Run("ordered concatenation", func(t *testing.T) {
		record, err := contextstate.Reassemble(ref, contextstate.RetentionSession, []byte("ab"), []byte("cd"), []byte("ef"))
		if err != nil {
			t.Fatalf("Reassemble: %v", err)
		}
		if !bytes.Equal(record.Data, whole) {
			t.Fatalf("Data = %q, want %q", record.Data, whole)
		}
		if err := record.Validate(); err != nil {
			t.Fatalf("reassembled record invalid: %v", err)
		}
	})
	t.Run("single chunk equals whole", func(t *testing.T) {
		record, err := contextstate.Reassemble(ref, contextstate.RetentionCompliance, whole)
		if err != nil {
			t.Fatalf("Reassemble: %v", err)
		}
		if !bytes.Equal(record.Data, whole) {
			t.Fatalf("Data = %q, want %q", record.Data, whole)
		}
	})
	t.Run("wrong total size fails closed", func(t *testing.T) {
		if _, err := contextstate.Reassemble(ref, contextstate.RetentionSession, []byte("abc")); err == nil {
			t.Fatal("Reassemble accepted a size mismatch")
		}
	})
	t.Run("digest mismatch fails closed", func(t *testing.T) {
		wrong := []byte("abcdeG")
		if _, err := contextstate.Reassemble(ref, contextstate.RetentionSession, wrong); err == nil {
			t.Fatal("Reassemble accepted a digest mismatch")
		}
	})
	t.Run("chunk order matters", func(t *testing.T) {
		if _, err := contextstate.Reassemble(ref, contextstate.RetentionSession, []byte("cd"), []byte("ab"), []byte("ef")); err == nil {
			t.Fatal("Reassemble accepted reordered chunks under one ref")
		}
	})
	t.Run("invalid ref fails closed", func(t *testing.T) {
		bad := ref
		bad.Namespace = ""
		if _, err := contextstate.Reassemble(bad, contextstate.RetentionSession, whole); err == nil {
			t.Fatal("Reassemble accepted an invalid ref")
		}
	})
	t.Run("empty retention fails closed", func(t *testing.T) {
		if _, err := contextstate.Reassemble(ref, "", whole); err == nil {
			t.Fatal("Reassemble accepted an empty retention class")
		}
	})
}
