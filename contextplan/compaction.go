package contextplan

import (
	"errors"
	"fmt"
)

// Compaction thresholds and bounds.
const (
	// DefaultTriggerPercent compacts at this percent of Window.Budget().
	DefaultTriggerPercent = 100
	// DefaultTargetPercent compacts down to this percent of Budget().
	DefaultTargetPercent = 10
	// DefaultRecentTail is the message-count bound of the tail fill.
	DefaultRecentTail = 8
	// MaxRecentTail is the highest tail bound a caller may set.
	MaxRecentTail = 64
	// CompactionAlgorithm names the idempotency-key fingerprint scheme.
	CompactionAlgorithm = "context-compact-v1"
)

// Sentinel errors for Compact; test with errors.Is.
var (
	// ErrNoMessages is Compact's error for an empty message list.
	ErrNoMessages = errors.New("contextplan: no messages to compact")
	// ErrEstimateFailed is Compact's error when the token estimator
	// fails.
	ErrEstimateFailed = errors.New("contextplan: token estimate failed")
	// ErrRetentionOverflow is Compact's error when the retention set
	// alone exceeds the window budget.
	ErrRetentionOverflow = errors.New("contextplan: retention set alone exceeds the window")
	// ErrNoObjective is Compact's error when no user message exists to
	// retain as the objective.
	ErrNoObjective = errors.New("contextplan: no user message to retain as objective")
)

// Compaction configures compaction thresholds and retention. The zero
// value means the defaults, never "disabled": TriggerPercent zero
// means DefaultTriggerPercent, TargetPercent zero means
// DefaultTargetPercent, RecentTail zero means DefaultRecentTail.
type Compaction struct {
	TriggerPercent int
	TargetPercent  int
	TargetTokens   int
	RecentTail     int
	PreserveNames  []string
}

// Validate rejects percents outside [0, 100], a negative TargetTokens
// or RecentTail, a RecentTail over MaxRecentTail, an empty but
// present PreserveNames entry, and duplicate PreserveNames entries.
// When TargetTokens is zero, a TargetPercent at or above the resolved
// TriggerPercent is rejected; when TargetTokens is positive, that
// comparison is skipped and Window.Validate instead rejects a
// TargetTokens at or above Budget().
func (c Compaction) Validate() error {
	if err := c.validatePercents(); err != nil {
		return err
	}
	if c.TargetTokens < 0 {
		return fmt.Errorf("contextplan: compaction target tokens %d is negative", c.TargetTokens)
	}
	if c.RecentTail < 0 {
		return fmt.Errorf("contextplan: compaction recent tail %d is negative", c.RecentTail)
	}
	if c.RecentTail > MaxRecentTail {
		return fmt.Errorf("contextplan: compaction recent tail %d over %d", c.RecentTail, MaxRecentTail)
	}
	return c.validatePreserveNames()
}

// validatePercents bounds both percents and the two-mode target rule.
func (c Compaction) validatePercents() error {
	if c.TriggerPercent < 0 || c.TriggerPercent > 100 {
		return fmt.Errorf("contextplan: trigger percent %d outside [0, 100]", c.TriggerPercent)
	}
	if c.TargetPercent < 0 || c.TargetPercent > 100 {
		return fmt.Errorf("contextplan: target percent %d outside [0, 100]", c.TargetPercent)
	}
	if c.TargetTokens == 0 && c.targetPercent() >= c.triggerPercent() {
		return fmt.Errorf("contextplan: target percent %d at or above trigger percent %d",
			c.targetPercent(), c.triggerPercent())
	}
	return nil
}

// validatePreserveNames rejects empty and duplicate entries.
func (c Compaction) validatePreserveNames() error {
	seen := make(map[string]struct{}, len(c.PreserveNames))
	for _, name := range c.PreserveNames {
		if name == "" {
			return errors.New("contextplan: preserve names entry is empty")
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("contextplan: preserve names entry %q is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// triggerPercent resolves TriggerPercent against its default.
func (c Compaction) triggerPercent() int {
	if c.TriggerPercent <= 0 {
		return DefaultTriggerPercent
	}
	return c.TriggerPercent
}

// targetPercent resolves TargetPercent against its default.
func (c Compaction) targetPercent() int {
	if c.TargetPercent <= 0 {
		return DefaultTargetPercent
	}
	return c.TargetPercent
}

// recentTailBound resolves RecentTail against its default.
func (c Compaction) recentTailBound() int {
	if c.RecentTail <= 0 {
		return DefaultRecentTail
	}
	return c.RecentTail
}
