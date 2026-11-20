# Discovery: write path — evidence pulled 2026-11-20

Companion to `2026-11-20-write-path-discovery-movement-1.md`. Read-only collection:
pod logs through the infra MCP, repo, git history, Gitea. No amounts are recorded here
on purpose; account numbers and counts only.

## Headline

**The system cannot currently show how much bookkeeping time a write would save.**
No usage or audit record of operator activity exists to measure it from. A1 and A2
(movement 1) are therefore untested, not supported and not refuted.

## What was found

### 1. No usage telemetry (decides how A1 can be tested)
- Migrations 001-008 hold no audit, usage, event or tool-call table. The only
  behavioural table is `fortnox_unbooked_seen` (#93), which stores which unbooked
  invoices were already reported, with no amounts or timestamps of operator action.
- The running server pod's log (restart 3 minutes before collection) shows start-up
  lines only. Tool and chat calls are not logged at request level.
- `.claude/rules/security.md` describes `~/.coo-agent/audit.log`. That belongs to the
  archived coo-agent and is not in this system.
- So "hours per month on manual bookkeeping" has no source in the system. It needs
  a deliberate measurement (see Next).

### 2. What the daily overview shows about the live company (read path works)
- `cobalt-dingo-overview` runs daily as a CronJob; the last three completed pods were
  roughly 24 h apart. It refuses to report if the token opens a different company
  than the named one.
- Latest report: **0 unbooked supplier invoices, 0 unbooked customer invoices**;
  1 supplier unpaid; 4 customer unpaid, all overdue; trial balance reconciles
  to zero. A backlog of unbooked invoices is not what is costing time at this snapshot.
  This is weak evidence *against* A1 as stated ("posting vouchers").
- Two settlement accounts hold non-zero closing balances: **2894 (Avräkning SEB kort)**
  and **2895 (Avräkning Mynt)**. Clearing accounts normally return to zero once card
  and wallet spend is matched to receipts and booked. A non-zero balance is consistent
  with unreconciled card/wallet spend, which is the reconciliation work in #62.
  This is a hypothesis to test, not a finding: the balance may be a normal timing
  difference.
- The report is produced from production-capable credentials, read-only.

### 3. Receipt volume is small
- Dry run over 2025-10-01 to 2026-09-10: 16,558 mails across three accounts,
  **24 candidates** the rules would forward, 8 more the rules miss (#78).
  Hand-forwarded receipts are excluded from the count, so the real volume is
  higher by an unknown amount.
- At roughly two receipts a month the receipt-routing step is not the large cost.

### 4. Where the engineering effort went (not the same as bookkeeping effort)
- 160 commits since 2026-09-01. Dominated by controls, CI and read-path work. Useful
  for #65 (the read path has had a lot of hardening) and irrelevant to A1.

### 5. A contradiction to resolve before any write work
- #88 states `FORTNOX_MODE=sandbox` sets `AllowsWrites = true` unconditionally.
  The live server's start-up line reads `fortnox_mode: sandbox`, `allows_writes: false`.
  Either #88's item 1 was fixed since it was written, or the deployment overrides it.
  Not determined here. Worth one look before #88 is relied on as a description of
  current behaviour.

## What this does to the assumptions

| # | Assumption | Evidence | State |
|---|---|---|---|
| A1 | Material share of time is posting vouchers we could propose | No telemetry; zero unbooked backlog | untested, mild counter-signal |
| A2 | A draft the operator posts by hand removes most of it | none | untested |
| A3 | Read path trustworthy enough to write from | Daily report reconciles; no false-discrepancy record exists | partly supported, unmeasured |
| A4 | Propose-confirm + counter-voucher is recoverable | design only (#45) | untested |
| A5 | Few low-risk voucher types suffice | none | untested |

## New candidate problem (not a write problem)

Settlement accounts 2894/2895 not at zero point at **reconciliation of card/wallet
spend against receipts** (#62, #61) as the larger time cost. If it is, the read-only
reconciliation work delivers the value before any write path is needed, and
the write question shrinks to the final posting step.

## Next (cheapest evidence first)

1. **Measure.** One calendar month of a plain time log of bookkeeping sessions
   (start, end, task, voucher or invoice type). No tooling. This answers A1.
2. **Check the clearing accounts.** Ask the accountant, or read the ledger for 2894/2895:
   how old is the oldest unmatched item, how many items, how long to clear a month.
3. **Resolve #88's AllowsWrites contradiction.**
4. **Only then** decide whether movement 2 needs instrumentation in the product
   (a request-level event log) so later phases are measured rather than recalled.
