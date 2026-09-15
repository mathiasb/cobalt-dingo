-- Dropping this loses only the memory of what was already reported. The next
-- run then treats every currently unbooked invoice as new and alerts once,
-- which is noisy but never silent — the safe direction for an alert.
DROP TABLE IF EXISTS fortnox_unbooked_seen;
