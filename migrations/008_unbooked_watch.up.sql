-- Remembers which invoices were unbooked at the last look, so a nightly run
-- can tell a NEW obligation from one already reported (#93).
--
-- The table mirrors "currently unbooked", not "ever unbooked": a booked
-- invoice is DELETED rather than flagged. That is deliberate. If an invoice
-- were kept forever, an invoice that is booked and later unbooked again could
-- never be reported a second time, and a recurrence is a real event.
--
-- No amount or due date. The watched condition is "a new unbooked invoice
-- appeared", so storing amounts would invite comparing them, which raises an
-- alert when an existing invoice is merely edited — a different event.
CREATE TABLE fortnox_unbooked_seen (
    tenant_id      TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    -- 'payable' or 'receivable'. Part of the identity: payable #144 and
    -- receivable #144 are different invoices, and keying on the number alone
    -- would suppress a real alert.
    kind           TEXT        NOT NULL,
    invoice_number INTEGER     NOT NULL,
    first_seen_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, kind, invoice_number)
);
