-- name: InsertToolEvent :exec
-- Append only. Metadata only: no arguments, amounts or counterparties.
INSERT INTO tool_events (at, tenant_id, actor, tool, class, status, duration_ms)
VALUES ($1, $2, $3, $4, $5, $6, $7);
