-- Dropping this discards the evidence trail for bet #139. Intended only for a
-- rollback of the migration itself, not as routine cleanup.
DROP TRIGGER IF EXISTS tool_events_no_update_delete ON tool_events;
DROP FUNCTION IF EXISTS tool_events_append_only();
DROP TABLE IF EXISTS tool_events;
