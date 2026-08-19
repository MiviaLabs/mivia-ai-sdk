package skills_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/skills"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		skill   skills.Skill
		wantErr error
	}{
		{
			name:    "blank name",
			skill:   skills.Skill{Name: "", Instructions: "do the thing"},
			wantErr: skills.ErrBlankName,
		},
		{
			name:    "whitespace-only name",
			skill:   skills.Skill{Name: "   ", Instructions: "do the thing"},
			wantErr: skills.ErrBlankName,
		},
		{
			name:    "blank instructions",
			skill:   skills.Skill{Name: "deploy", Instructions: ""},
			wantErr: skills.ErrBlankInstructions,
		},
		{
			name:    "whitespace-only instructions",
			skill:   skills.Skill{Name: "deploy", Instructions: "  "},
			wantErr: skills.ErrBlankInstructions,
		},
		{
			name: "blank trigger entry",
			skill: skills.Skill{
				Name:         "deploy",
				Instructions: "do the thing",
				Triggers:     []string{"deploy", "  "},
			},
			wantErr: skills.ErrBlankTrigger,
		},
		{
			name: "duplicate trigger entry, case-insensitive",
			skill: skills.Skill{
				Name:         "deploy",
				Instructions: "do the thing",
				Triggers:     []string{"Deploy", "deploy"},
			},
			wantErr: skills.ErrDuplicateTrigger,
		},
		{
			name: "empty triggers pass",
			skill: skills.Skill{
				Name:         "deploy",
				Instructions: "do the thing",
				Triggers:     nil,
			},
			wantErr: nil,
		},
		{
			name: "fully populated skill passes, no check on RequiredTools",
			skill: skills.Skill{
				Name:          "deploy",
				Instructions:  "do the thing",
				Triggers:      []string{"deploy", "release"},
				RequiredTools: []string{"shell", "not-a-real-tool"},
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.skill.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
