// Package longtermmemory_test exercises the tiered entry store.
package longtermmemory_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

// validEntry builds one valid entry with the given title and summary.
func validEntry(title, summary string) longtermmemory.Entry {
	return longtermmemory.Entry{
		Title:   title,
		Scope:   "proj",
		Verdict: longtermmemory.VerdictGood,
		Tags:    []string{"go"},
		Created: "2026-01-15",
		Summary: summary,
		Detail:  "detail text",
	}
}

// entryCase is one Validate table row.
type entryCase struct {
	name    string
	entry   longtermmemory.Entry
	wantErr bool
}

// runEntryCases runs one table of Validate cases.
func runEntryCases(t *testing.T, cases []entryCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.entry.Validate()
			if c.wantErr && err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestEntryValidateTitleScopeVerdict(t *testing.T) {
	blankScope := validEntry("t", "s")
	blankScope.Scope = " "
	badVerdict := validEntry("t", "s")
	badVerdict.Verdict = "great"
	neutral := validEntry("t", "s")
	neutral.Verdict = longtermmemory.VerdictNeutral
	runEntryCases(t, []entryCase{
		{name: "valid full entry", entry: validEntry("Title", "Summary")},
		{name: "invalid empty title", entry: validEntry("", "s"), wantErr: true},
		{name: "invalid whitespace title", entry: validEntry("   ", "s"), wantErr: true},
		{name: "invalid title over 120 runes", entry: validEntry(strings.Repeat("t", 121), "s"), wantErr: true},
		{name: "valid title at 120 runes", entry: validEntry(strings.Repeat("t", 120), "s")},
		{name: "invalid title with line break", entry: validEntry("two\nlines", "s"), wantErr: true},
		{name: "invalid blank scope", entry: blankScope, wantErr: true},
		{name: "invalid unknown verdict", entry: badVerdict, wantErr: true},
		{name: "valid each verdict", entry: neutral},
	})
}

func TestEntryValidateTextBounds(t *testing.T) {
	longDetail := validEntry("t", "s")
	longDetail.Detail = strings.Repeat("d", 2001)
	ctrlSummary := validEntry("t", "bad\x02char")
	ctrlDetail := validEntry("t", "s")
	ctrlDetail.Detail = "bad\x03char"
	newlines := validEntry("t", "line\nsecond\ttab")
	newlines.Detail = "d\nx\ty"
	atLimit := validEntry("t", strings.Repeat("s", 400))
	runEntryCases(t, []entryCase{
		{name: "invalid empty summary", entry: validEntry("t", ""), wantErr: true},
		{name: "invalid summary over 400 runes", entry: validEntry("t", strings.Repeat("s", 401)), wantErr: true},
		{name: "valid summary at 400 runes", entry: atLimit},
		{name: "invalid detail over 2000 runes", entry: longDetail, wantErr: true},
		{name: "valid LF and TAB in summary and detail", entry: newlines},
		{name: "invalid other control character in summary", entry: ctrlSummary, wantErr: true},
		{name: "invalid other control character in detail", entry: ctrlDetail, wantErr: true},
	})
}

func TestEntryValidateTagsAndDate(t *testing.T) {
	nineTags := validEntry("t", "s")
	nineTags.Tags = nTags(9)
	eightTags := validEntry("t", "s")
	eightTags.Tags = nTags(8)
	longTag := validEntry("t", "s")
	longTag.Tags = []string{strings.Repeat("g", 33)}
	commaTag := validEntry("t", "s")
	commaTag.Tags = []string{"a,b"}
	breakTag := validEntry("t", "s")
	breakTag.Tags = []string{"a\nb"}
	badDate := validEntry("t", "s")
	badDate.Created = "15-01-2026"
	noDate := validEntry("t", "s")
	noDate.Created = ""
	runEntryCases(t, []entryCase{
		{name: "invalid nine tags", entry: nineTags, wantErr: true},
		{name: "valid eight tags", entry: eightTags},
		{name: "invalid tag over 32 runes", entry: longTag, wantErr: true},
		{name: "invalid tag with comma", entry: commaTag, wantErr: true},
		{name: "invalid tag with line break", entry: breakTag, wantErr: true},
		{name: "invalid created date", entry: badDate, wantErr: true},
		{name: "valid empty created", entry: noDate},
	})
}

// nTags builds n distinct valid tags.
func nTags(n int) []string {
	tags := make([]string, n)
	for i := range tags {
		tags[i] = strings.Repeat("g", i+1)
	}
	return tags
}
