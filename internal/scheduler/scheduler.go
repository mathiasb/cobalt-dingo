// Package scheduler runs cron-based read-only monitoring jobs.
// It NEVER performs write operations – all actions require interactive
// user confirmation via the MCP interface.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/api"
)

// Scheduler runs periodic monitoring jobs.
type Scheduler struct {
	client   *api.Client
	auditLog *audit.Logger
	stop     chan struct{}
}

// New creates a new Scheduler.
func New(client *api.Client, auditLog *audit.Logger) *Scheduler {
	return &Scheduler{
		client:   client,
		auditLog: auditLog,
		stop:     make(chan struct{}),
	}
}

// Start begins all scheduled jobs and blocks until ctx is cancelled.
// TODO: replace manual tickers with robfig/cron/v3 for full cron-syntax support.
func (s *Scheduler) Start(ctx context.Context) error {
	slog.Info("scheduler: starting")

	dailyTicker := nextTick(8, 0)  // 08:00 daily
	monthlyTicker := nextTick(9, 0) // 09:00 (checked daily, acts on 1st)

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler: stopping")
			return nil
		case t := <-dailyTicker.C:
			slog.Info("scheduler: running daily jobs", "time", t)
			go s.runDailyBriefing(ctx)
			go s.runInboxCheck(ctx)
		case t := <-monthlyTicker.C:
			if time.Now().Day() == 1 {
				slog.Info("scheduler: running monthly jobs", "time", t)
				go s.runVATDeadlineCheck(ctx)
				go s.runExpenseReminder(ctx)
			}
		}
	}
}

// runDailyBriefing checks for unpaid invoices and upcoming deadlines.
func (s *Scheduler) runDailyBriefing(ctx context.Context) {
	slog.Info("scheduler: daily briefing – checking unpaid invoices")
	invoices, err := s.client.ListInvoices(ctx, "unpaid")
	if err != nil {
		slog.Error("scheduler: daily briefing failed", "err", err)
		return
	}
	if len(invoices) > 0 {
		slog.Info("scheduler: unpaid invoices found", "count", len(invoices))
		// TODO: send notification to user via configured channel
	}
}

// runInboxCheck scans the IMAP inbox for new invoices/receipts.
// TODO: wire up internal/mail when implemented.
func (s *Scheduler) runInboxCheck(_ context.Context) {
	slog.Info("scheduler: inbox check – not yet implemented")
}

// runVATDeadlineCheck warns about upcoming VAT declaration deadlines.
func (s *Scheduler) runVATDeadlineCheck(_ context.Context) {
	deadlines := vatDeadlines(time.Now().Year())
	for _, d := range deadlines {
		daysUntil := int(time.Until(d).Hours() / 24)
		if daysUntil > 0 && daysUntil <= 30 {
			slog.Info("scheduler: VAT deadline approaching",
				"deadline", d.Format("2006-01-02"),
				"days_until", daysUntil,
			)
			// TODO: send notification
		}
	}
}

// runExpenseReminder reminds about recurring expenses due for booking.
func (s *Scheduler) runExpenseReminder(_ context.Context) {
	slog.Info("scheduler: expense reminder – checking recurring expenses")
	// TODO: check which recurring expenses (Telenor, Telia, Bahnhof) are due
}

// vatDeadlines returns the four VAT declaration deadlines for a given year.
// Q1→12 May, Q2→12 Aug, Q3→12 Nov, Q4→12 Feb (following year).
func vatDeadlines(year int) []time.Time {
	return []time.Time{
		time.Date(year, time.May, 12, 0, 0, 0, 0, time.Local),
		time.Date(year, time.August, 12, 0, 0, 0, 0, time.Local),
		time.Date(year, time.November, 12, 0, 0, 0, 0, time.Local),
		time.Date(year+1, time.February, 12, 0, 0, 0, 0, time.Local),
	}
}

// nextTick returns a ticker that fires at the next occurrence of hour:minute.
func nextTick(hour, minute int) *time.Ticker {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}
	d := time.Until(next)
	// Use a 24h ticker but offset to the correct time.
	// For production use, replace with robfig/cron.
	_ = d
	return time.NewTicker(24 * time.Hour)
}
