-- One row per MCP tool or chat tool call, for the bet #139 evidence trail (#140).
--
-- Metadata only. There is deliberately NO column for arguments, amounts,
-- counterparty names, document contents or tokens: a log that can hold them
-- will, eventually, hold them. Adding a column here is a reviewed change.
--
-- No foreign key to tenants. This is evidence, and ON DELETE CASCADE would let
-- removing a tenant erase the record of what was done for it.
--
-- Append-only: updates and deletes are refused by trigger. A table owner can
-- still drop the trigger; this stops accidents, not a deliberate owner.
CREATE TABLE tool_events (
    id          BIGSERIAL   PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL,
    tenant_id   TEXT        NOT NULL,
    actor       TEXT        NOT NULL DEFAULT '',
    tool        TEXT        NOT NULL,
    class       TEXT        NOT NULL CHECK (class IN ('read', 'write')),
    status      TEXT        NOT NULL CHECK (status IN ('ok', 'error', 'refused')),
    duration_ms BIGINT      NOT NULL CHECK (duration_ms >= 0)
);

CREATE INDEX tool_events_at_idx ON tool_events (at);

CREATE FUNCTION tool_events_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'tool_events is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tool_events_no_update_delete
    BEFORE UPDATE OR DELETE ON tool_events
    FOR EACH ROW EXECUTE FUNCTION tool_events_append_only();
