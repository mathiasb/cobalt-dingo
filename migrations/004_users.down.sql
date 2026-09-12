-- Dropping users breaks every credential key derived from it: stored Fortnox
-- tokens are keyed "<user_id>:<mode>:<company>" and the ids are not
-- reconstructible from anywhere else. Rolling this back therefore orphans
-- every connection, which is the exact failure ADR-0003 was written to stop.
-- Reconnecting each company is the recovery path.
DROP TABLE IF EXISTS user_subjects;
DROP TABLE IF EXISTS users;
