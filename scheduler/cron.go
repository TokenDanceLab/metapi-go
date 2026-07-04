package scheduler

import (
	"context"
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
)

// ValidateCronExpr validates a cron expression using robfig/cron parser
// with seconds field. Returns true if the expression is valid.
func ValidateCronExpr(expr string) bool {
	if strings.TrimSpace(expr) == "" {
		return false
	}
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err == nil
}

// ParseCronExpr tries to parse a cron expression. Returns error if invalid.
func ParseCronExpr(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return fmt.Errorf("empty cron expression")
	}
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

// addJob adds a cron job with panic recovery. Returns the entry ID and error.
func (cr *cronRunner) addJob(spec string, fn func()) (cron.EntryID, error) {
	return cr.cron.AddFunc(spec, func() {
		defer func() {
			if r := recover(); r != nil {
				// Panic recovered; logged inside the job itself
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
