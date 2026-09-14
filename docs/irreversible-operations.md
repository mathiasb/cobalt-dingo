# What cannot be undone in the live books

Written 2026-09-14, at Mathias's insistence, before the first production
connection. He asked me to demonstrate I understand that some production
actions cannot be cleaned up without a lot of extra work.

**The asymmetry is the point.** Almost every risk in this repo so far has been
recoverable: a wrong company connected read-only is disconnectable, a bad
config is a rollback, a lost token is a reconnect. Bookkeeping is not like
that. A mistake in the books is not deleted, it is **corrected** — and both the
mistake and the correction remain visible for ever.

So "we can fix it afterwards" is false here, and any design that relies on it
is wrong. Reversal cost, not reversibility, is the thing to weigh.

## Tier 1 — cannot be removed, only corrected

| Operation | Why it cannot be undone | Cost of the correction |
|---|---|---|
| **Bookkeeping a voucher** (`/3/vouchers`, bookkeeping a supplier or customer invoice) | Swedish bookkeeping law requires an intact audit trail. A posted voucher is not deletable. | A correcting voucher (*rättelseverifikation*). The original stays. Both appear in the ledger and in any report of that period, for ever. |
| **Consuming a voucher number** | Series numbering is sequential and gapless. A number, once issued, is not reused — even for an entry that is immediately corrected. | None available. The gap in *meaning* is permanent even though the sequence stays intact. A stray batch permanently pollutes the series. |
| **Posting into a closed period** | If the financial year is closed, or the VAT for that period already declared to Skatteverket, the books and the filed figures diverge. | Reopening the year, possibly via the accountant; a corrected VAT declaration; in the worst case an amended annual report to Bolagsverket. |

## Tier 2 — reaches a third party, so there is nothing to roll back

| Operation | Why it cannot be undone |
|---|---|
| **Sending a customer invoice** | The customer has received it. A credit note (*kreditfaktura*) cancels the amount, not the fact that it arrived — and consumes another invoice number. |
| **Registering a payment** (`/3/invoicepayments`, `/3/supplierinvoicepayments`) | Marks an invoice settled and, where a bank file follows, moves money. Reversal is another posting plus, potentially, recovering funds from a counterparty. |
| **Forwarding to arkivplats** | Already true today for receipts. SMTP accepts and Fortnox has the document; there is no unsend. |

## Tier 3 — quiet, and therefore arguably worse

| Operation | Why it is dangerous |
|---|---|
| **Editing supplier bank details** | Changing an IBAN or BIC does not look destructive and is not flagged anywhere. Every future payment to that supplier goes to the new account. The damage appears later, in money, and is attributed to something else. |
| **Deleting a file from the Arkivplats** (`DELETE /3/inbox/{Id}`) | Granted by the `inbox` scope added 2026-09-14. A forwarded receipt may exist **only** there — the mail it came from can be archived or gone. Unlike a voucher there is no audit trail to correct from; the document is simply lost. |
| **Cancelling a supplier invoice** | `cmd/e2e-teardown` does exactly this by design. Against real books it removes a recorded liability. |

## What currently blocks each tier

Stated so the protection is auditable rather than assumed. Verified 2026-09-14.

| Guard | Blocks | Depth |
|---|---|---|
| `payment` scope **not requested** | Tier 2 payments | Vendor-side. The token cannot do it at all. The only guard here not of our own making. |
| `BuildMCPDeps` refuses `IsProduction() && AllowsWrites` | all tiers, via the server | Refuses to construct the adapters. Setting the flag breaks the build path rather than opening writes. |
| `Client.do` write gate | all tiers, every call | Rejects non-GET/HEAD before any HTTP traffic. Single chokepoint; every `/3/` call routes through it. |
| `FORTNOX_PRODUCTION_ALLOW_WRITES` absent | all tiers | Config. The weakest of the four, because it is one string. |
| `fortnox.AssertCompany` | e2e tooling only | Refuses to run against any company but the named test one. |
| Chat tool surface | the LLM path | All 38 tools are reads. No create, update or delete verb exists for the model to call. |

**Note what is not in that list.** Fortnox has no read-only scopes — *"All scopes
gives both read and write access and it is not possible to only have read access
through the API."* Except for `payment`, every guard above is ours. The token
itself can write the books.

## The rule this produces

1. **A write to production needs a superseding ADR, not a flag.** ADR-0001 says
   so and `BuildMCPDeps` enforces it. #65 is where "proven enough" gets defined.
2. **When writes are eventually allowed, Tier 1 and Tier 2 need a confirmation
   step that names the specific effect** — not a generic "are you sure". The
   difference between "post 1 voucher" and "post 340 vouchers" is the whole
   risk, and a yes/no dialog hides it.
3. **Tier 3 deserves more protection than Tier 1, not less.** Its operations
   look harmless, carry no audit trail to correct from, and fail silently. The
   inbox DELETE is the clearest case: it can destroy the only copy of a receipt.
4. **Never reason about a batch from a single-item test.** Rate limiting and
   idempotency are correctness requirements here, not performance ones — a
   retried POST that lands twice is two vouchers.

## Where I am least confident

The Swedish regulatory specifics — when a year counts as closed, what forces an
amended annual report, whether a corrected VAT declaration is needed for a
given change. Mathias knows this and I do not. **Where this document and his
judgement disagree, he is right**, and the entries above should be treated as
the shape of the problem rather than as advice.
