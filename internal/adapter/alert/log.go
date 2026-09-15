// Package alert delivers alerts raised by the domain.
//
// Only a log channel today. That is a deliberate stopping point rather than an
// oversight: a Gitea issue is the estate's convention for something that must
// outlive nobody looking, and granting this workload a write-capable Gitea
// token is a privilege change for a read-only reporting job — a decision worth
// making explicitly rather than inside a commit (#93).
package alert

import (
	"context"
	"log/slog"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

var _ domain.Alerter = (*LogAlerter)(nil)

// LogAlerter writes alerts to the log at ERROR level.
//
// ERROR, not WARN, because this is the only channel: the level is what makes
// it findable, and a WARN in a nightly Job log is indistinguishable from
// routine noise.
type LogAlerter struct{ log *slog.Logger }

// NewLogAlerter returns a LogAlerter writing to log.
func NewLogAlerter(log *slog.Logger) *LogAlerter { return &LogAlerter{log: log} }

// Alert implements domain.Alerter. It cannot fail, which is worth stating
// plainly: this channel offers no delivery guarantee at all. It puts the text
// where a human or a log scraper can find it, and nothing more. A channel that
// cannot report failure also cannot be trusted to have succeeded.
func (a *LogAlerter) Alert(_ context.Context, subject, body string) error {
	a.log.Error("ALERT: "+subject, "body", body)
	return nil
}
