package agentloop_test

// WorkBudget hook tests: reserve-before-call and refund-after-usage on
// a successful turn, no hook calls when Options.WorkBudget is nil,
// hard-fail before the Completer call when Reserve errors, full refund
// on a zero-usage error path, and Validate rejecting a half-wired
// WorkBudget.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// budgetLog records every WorkBudget hook call in order, with the
// usage each Refund carried.
type budgetLog struct {
	events []string
	usages []provider.Usage
}

func (b *budgetLog) hook() *agentloop.WorkBudget {
	return &agentloop.WorkBudget{
		Reserve: func(ctx context.Context, req provider.Request) error {
			b.events = append(b.events, "reserve")
			return nil
		},
		Refund: func(ctx context.Context, req provider.Request, used provider.Usage) {
			b.events = append(b.events, "refund")
			b.usages = append(b.usages, used)
		},
	}
}

// TestWorkBudgetReserveThenRefundOnSuccessfulTurn proves one iteration
// of a successful, no-tool-call run fires Reserve exactly once before
// the Completer call and Refund exactly once after it, with the
// response's real Usage.
func TestWorkBudgetReserveThenRefundOnSuccessfulTurn(t *testing.T) {
	usage := provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi there"), Usage: usage},
	}}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
	log := &budgetLog{}
	loop, err := agentloop.New(agentloop.Options{
		Completer:  completer,
		Tools:      reg,
		WorkBudget: log.hook(),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	if len(log.events) != 2 || log.events[0] != "reserve" || log.events[1] != "refund" {
		t.Fatalf("events = %v, want [reserve refund]", log.events)
	}
	if len(log.usages) != 1 || log.usages[0] != usage {
		t.Fatalf("refund usage = %+v, want %+v", log.usages, usage)
	}
}

// TestWorkBudgetNilHookStillRuns proves a nil WorkBudget changes
// nothing: the run completes and no hook fires (no panic).
func TestWorkBudgetNilHookStillRuns(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "done")},
	}}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

// TestWorkBudgetReserveErrorFailsClosed proves a Reserve error
// hard-fails the run BEFORE the Completer call runs, with no Refund.
func TestWorkBudgetReserveErrorFailsClosed(t *testing.T) {
	errRefused := errors.New("host: work limit exceeded")
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "never")},
	}}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
	log := &budgetLog{}
	budget := log.hook()
	budget.Reserve = func(ctx context.Context, req provider.Request) error { return errRefused }
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, WorkBudget: budget})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err == nil || !errors.Is(err, errRefused) {
		t.Fatalf("err = %v, want wrap of errRefused", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("completer calls = %d, want 0 (reserve fails before the call)", completer.callCount())
	}
	if len(log.events) != 0 {
		t.Fatalf("events = %v, want none after a failed reserve", log.events)
	}
}

// TestWorkBudgetRefundsZeroUsageOnChatError proves a failed Completer
// call refunds the never-consumed reservation with the zero Usage.
func TestWorkBudgetRefundsZeroUsageOnChatError(t *testing.T) {
	completer := &scriptedCompleter{errs: []error{errBoom}}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
	log := &budgetLog{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, WorkBudget: log.hook()})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if len(log.events) != 2 || log.events[1] != "refund" {
		t.Fatalf("events = %v, want [reserve refund]", log.events)
	}
	if len(log.usages) != 1 || log.usages[0] != (provider.Usage{}) {
		t.Fatalf("refund usage = %+v, want zero Usage", log.usages)
	}
}

// TestWorkBudgetValidateRequiresBothFuncs proves Options.Validate
// rejects a WorkBudget with either hook function missing.
func TestWorkBudgetValidateRequiresBothFuncs(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
	completer := &scriptedCompleter{}
	base := agentloop.Options{Completer: completer, Tools: reg}
	noReserve := base
	noReserve.WorkBudget = &agentloop.WorkBudget{Refund: func(context.Context, provider.Request, provider.Usage) {}}
	if err := noReserve.Validate(); err == nil || !strings.Contains(err.Error(), "WorkBudget") {
		t.Fatalf("err = %v, want WorkBudget validation error", err)
	}
	noRefund := base
	noRefund.WorkBudget = &agentloop.WorkBudget{Reserve: func(context.Context, provider.Request) error { return nil }}
	if err := noRefund.Validate(); err == nil || !strings.Contains(err.Error(), "WorkBudget") {
		t.Fatalf("err = %v, want WorkBudget validation error", err)
	}
}

// TestWorkBudgetReserveAndRefundOnPromptTooLongRecovery proves that
// PromptTooLong recovery refunds the initial failed request and reserves
// for the retry request.
func TestWorkBudgetReserveAndRefundOnPromptTooLongRecovery(t *testing.T) {
	usage := provider.Usage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12}
	completer := &scriptedCompleter{
		errs: []error{provider.ErrPromptTooLong, nil},
		responses: []provider.Response{
			{},
			{Message: textMessage(provider.RoleAssistant, "recovered"), Usage: usage},
		},
	}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
	log := &budgetLog{}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	sum := &summaryScript{}
	summarizer, err := contextsummary.NewSummarizer(sum)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer:  completer,
		Tools:      reg,
		WorkBudget: log.hook(),
		Window:     &w,
		Summarizer: summarizer,
		Calibrated: contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	if len(log.events) != 4 || log.events[0] != "reserve" || log.events[1] != "refund" || log.events[2] != "reserve" || log.events[3] != "refund" {
		t.Fatalf("events = %v, want [reserve refund reserve refund]", log.events)
	}
	if len(log.usages) != 2 || log.usages[0] != (provider.Usage{}) || log.usages[1] != usage {
		t.Fatalf("usages = %+v, want [zero %+v]", log.usages, usage)
	}
}

// TestWorkBudgetSettleSkipsZeroUsageRefund proves a successful completion with
// all-zero Usage skips Refund, keeping the reservation consumed.
func TestWorkBudgetSettleSkipsZeroUsageRefund(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "done"), Usage: provider.Usage{}},
	}}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
	log := &budgetLog{}
	loop, err := agentloop.New(agentloop.Options{
		Completer:  completer,
		Tools:      reg,
		WorkBudget: log.hook(),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	if len(log.events) != 1 || log.events[0] != "reserve" {
		t.Fatalf("events = %v, want [reserve] (no refund for zero usage)", log.events)
	}
	if len(log.usages) != 0 {
		t.Fatalf("usages = %v, want empty", log.usages)
	}
}

// TestWorkBudgetSettleUsageTable tests that any non-zero token field triggers Refund
// while all-zero token fields skip Refund.
func TestWorkBudgetSettleUsageTable(t *testing.T) {
	tests := []struct {
		name       string
		usage      provider.Usage
		wantRefund bool
	}{
		{
			name:       "all zero",
			usage:      provider.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
			wantRefund: false,
		},
		{
			name:       "prompt tokens only",
			usage:      provider.Usage{PromptTokens: 5, CompletionTokens: 0, TotalTokens: 0},
			wantRefund: true,
		},
		{
			name:       "completion tokens only",
			usage:      provider.Usage{PromptTokens: 0, CompletionTokens: 5, TotalTokens: 0},
			wantRefund: true,
		},
		{
			name:       "total tokens only",
			usage:      provider.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 5},
			wantRefund: true,
		},
		{
			name:       "all non-zero",
			usage:      provider.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
			wantRefund: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completer := &scriptedCompleter{responses: []provider.Response{
				{Message: textMessage(provider.RoleAssistant, "done"), Usage: tt.usage},
			}}
			reg := tools.New()
			mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
			log := &budgetLog{}
			loop, err := agentloop.New(agentloop.Options{
				Completer:  completer,
				Tools:      reg,
				WorkBudget: log.hook(),
			})
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if tt.wantRefund {
				if len(log.events) != 2 || log.events[1] != "refund" {
					t.Fatalf("events = %v, want [reserve refund]", log.events)
				}
				if len(log.usages) != 1 || log.usages[0] != tt.usage {
					t.Fatalf("refund usage = %+v, want %+v", log.usages, tt.usage)
				}
			} else {
				if len(log.events) != 1 || log.events[0] != "reserve" {
					t.Fatalf("events = %v, want [reserve]", log.events)
				}
			}
		})
	}
}
