package scheduler

import (
	"context"
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
)

// normalizeCronExpr auto-detects 5-field cron expressions (minute hour dom month dow)
// and prepends "0 " to make them 6-field (second minute hour dom month dow).
// This ensures compatibility with cron.WithSeconds() while accepting 5-field
// expressions commonly stored by the TypeScript frontend.
func normalizeCronExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return expr
	}
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		return "0 " + expr
	}
	return expr
}

// ValidateCronExpr validates a cron expression using robfig/cron parser
// with seconds field. Auto-converts 5-field expressions to 6-field.
// Returns true if the expression is valid.
func ValidateCronExpr(expr string) bool {
	if strings.TrimSpace(expr) == "" {
		return false
	}
	expr = normalizeCronExpr(expr)
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err == nil
}

// ParseCronExpr tries to parse a cron expression. Auto-converts 5-field
// expressions to 6-field. Returns error if invalid.
func ParseCronExpr(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return fmt.Errorf("empty cron expression")
	}
	expr = normalizeCronExpr(expr)
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err
}

// cronRunner wraps a robfig/cron scheduler with panic-safe job execution.
type cronRunner struct {
	cron *cron.Cron
}

// newCronRunner creates a new cron runner with seconds field support.
func newCronRunner() *cronRunner {
	return &cronRunner{
		cron: cron.New(cron.WithSeconds()),
	}
}

// addJob adds a cron job with panic recovery. Auto-converts 5-field cron
// expressions to 6-field for compatibility with cron.WithSeconds().
// Returns the entry ID and error.
func (cr *cronRunner) addJob(spec string, fn func()) (cron.EntryID, error) {
	spec = normalizeCronExpr(spec)
	return cr.cron.AddFunc(spec, func() {
		defer func() {
			if r := recover(); r != nil {
				_ = r // panic recovered; logged inside the job itself
			}
		}()
		fn()
	})
}

// removeJob removes a cron job by entry ID.
func (cr *cronRunner) removeJob(id cron.EntryID) {
	cr.cron.Remove(id)
}

// start begins executing scheduled jobs.
func (cr *cronRunner) start() {
	cr.cron.Start()
}

// stop returns a context that is done when all running jobs have completed.
func (cr *cronRunner) stop() context.Context {
	return cr.cron.Stop()
}
