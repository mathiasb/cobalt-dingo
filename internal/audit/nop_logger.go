package audit

// NopLogger is a Logger that silently discards all entries.
// Use it when audit logging is intentionally disabled (e.g. CLI tools, one-off scripts).
type NopLogger struct{}

// Log discards the entry and always returns nil.
func (NopLogger) Log(_ Entry) error { return nil }
