# Invoice processing service: target design (v2, after review)

Status: **DRAFT for a decision, 2026-11-25.** Author: agent. Owner: Mathias. Proposed ADR: `docs/adr/0013-invoice-processing-service-direction.md`.
v1 was reviewed by six independent agents with different lenses (design, scope, contradictions, implementation readiness, code accuracy, a cold read). Section 11 records what was taken and what was discarded and why.

## 0. What you are asked to decide

Approve **only** these four things. Everything else here is direction, not commitment.

1. **The direction and the principles in section 3.**
2. **Scope: propose-only, two output rungs only** (on-screen/CSV and a SIE4 file the user imports). No rung that writes to a ledger. ADR-0001 stays in force.
3. **A new document port for invoices.** ADR-0002's seam is a bank-statement parser; an invoice is not a transaction (section 6).
4. **Three spikes before any further build** (section 10): a read-only Fortnox probe on the company's own books, a text-layer versus vision comparison on real scans, and a day-one backtest on real history.

## 1. Terms

- **Tenant**: one company whose books the service reads (`TenantID` in the code). An accounting firm serving many companies is many tenants with shared users; whether that is the primary shape is open (Q5).
- **Owner**: Mathias for now; for a customer it would be the tenant's responsible person. Thresholds are owned by the tenant's owner.
- **ERP**: the accounting system. Fortnox here.
- **BAS**: the Swedish standard chart of accounts. 2440 supplier ledger, 2641 input VAT, 3740 öres- och kronutjämning (rounding), 2645/2614 input/output VAT for reverse charge. Account labels were seen in the live company's chart via the daily overview; BAS meaning is not independently cited here.
- **Reverse charge**: for some foreign purchases the buyer accounts for both input and output VAT.
- **SIE4**: the Swedish accounting interchange file format. Details (CP437 encoding, `#FLAGGA`) come from a brain note, not verified in this repo.
- **Top-1 / top-3**: the right account is the first / within the first three proposed.
- **Held-out, time-split**: train on the earlier bookings, test on the latest ones, never a random split.
- **Lane**: *propose* (confident, pre-ticked for a human), *review* (a human decides), *none* (no proposal made).
- **Bet #142**: the placed bet (2026-11-24) to prove an invoice processor that learns from a customer's own history. **ADR-0001**: live books are read-only. **ADR-0009**: the venture framing is paused.

## 2. Problem and non-goals

**Problem.** Supplier invoices arrive as PDFs, images, e-invoices and mail attachments. Someone codes each: supplier, expense account, VAT treatment, cost centre, then books it. The coding is repetitive, rule-heavy and company-specific, and mistakes in the books are permanent: a posted voucher is corrected, never deleted (`docs/irreversible-operations.md`).

**Goal.** Turn an invoice into a *proposed* accounts payable entry, learn each tenant's way of booking from its own history and corrections, and say with measured evidence how often it is right for that tenant.

**Non-goals now.** Posting to a live ledger, payment initiation, FX conversion, fine-tuning a model, selling it (ADR-0009: no buyer work).

## 3. Principles

1. **Propose, never post.** Approval is a human act bound to the exact artifact and is absent from any agent surface (ADR-0007). "Auto" means a pre-ticked proposal, never a post.
2. **Structured first, vision last.** e-invoice XML, then the PDF's text layer, then the vision model. Consume capabilities rather than build ML pipelines (brain, 2026-06-01). An upstream vendor's reading is one more extractor, used as a second source.
3. **Identity before similarity.** Resolve the supplier by organisation number, VAT number, bank account or the ERP's supplier number; names are the fallback.
4. **Rules validate the classifier; the classifier never decides tax.** The tax rule table admits only (account, VAT code) pairs allowed for that supplier's country and type. A pair that is ambiguous (reverse charge, import) stays in review by construction.
5. **Every number has a second source.** Net + VAT = total, per VAT rate; the text layer agrees with the vision reading where both exist. Disagreement is a refusal, not a guess.
6. **A proposal must cite its evidence.** A candidate is offered only if it can name at least one past voucher or supplier-invoice that supports it. Explanation is a precondition of the output, not a property of the model.
7. **Evidence before automation, per supplier.** Backtest on the tenant's imported history on day one; open the pre-ticked lane per supplier once that supplier has enough consistent past bookings (threshold set from the backtest, section 9), not per tenant.
8. **Local models only for customer documents.** Hosted models only for public or synthetic data.

## 4. Pipeline

```
 Segment ─▶ DocType ─▶ Extract ─▶ SupplierResolve ─▶ Validate ─▶ Rules ─▶ Classify ─▶ Build ─▶ Review ─▶ Sink
 (pages,     (invoice,   (e-invoice  (org.nr, VAT no.,  (lines, per-   (VAT pair,  (account     (balanced  (accept/   (CSV,
  several     credit      XML > text  bank acct, ERP    rate VAT,      period      only, cites   exact      correct/   SIE4)
  invoices    note,       layer >     supplier no.,     rounding,      lock,       evidence)     integers)  reject)
  per file)   reminder)   vision)     then name)        2-source)      duplicate)
```

Order matters: the supplier decides the VAT regime, credit-note handling and the duplicate key, so it is resolved before validation and rules; the classifier picks the account last and is constrained by the rules.

| Stage | Refuses when | Reuse / today |
|---|---|---|
| Segment | pages missing, total without lines, several invoices that cannot be split | new |
| DocType | not an invoice (credit note is handled, quote and reminder are refused) | new |
| Extract | no source yields the mandatory fields; sources disagree | VLM extractor exists (`internal/extract`); mail/IMAP/PDF in `internal/receipts` |
| SupplierResolve | identifiers conflict | new; `domain.Supplier` holds only IBAN/BIC today, the Fortnox spec lists `OrganisationNumber`, `VATNumber`, `BankAccountNumber`, `PreDefinedAccount` |
| Validate | amounts unbounded or unreconciled per rate; date not parseable with an explicit order | exists for a single VAT rate (`Reconciled`, `ParseDate`, `MaxAmountMinor`); per-rate lines are new |
| Rules | account not in the tenant's chart, period closed or VAT-declared, VAT pair not admissible, **duplicate** | chart-account check partly in `internal/receipts/validator` (uses float64, rewrite on integers); rest new |
| Classify | no candidate can cite evidence, or best candidate is below the supplier's threshold | supplier-text retrieval exists (`internal/booking`); resolver-keyed version is new |
| Build | does not balance, any line zero or negative | exists (`internal/apentry`, big-integer check) |
| Review | n/a | accept/correct page exists (`internal/demo`) |
| Sink | n/a | CSV exists; SIE4 is new |

**Duplicate key** (one place, after SupplierResolve): tenant + resolved supplier + normalised invoice number + gross amount + currency, with the document hash as a secondary signal. Checked against the ERP's existing supplier invoices as well as our own history. A hit is a hard refusal that links the original.

**Credit notes, mixed rates, multi-page**: the invoice is a list of lines with a VAT rate each; credit notes carry negative amounts through the whole pipeline and produce the mirrored entry; anything not fully read is a refusal.

**Closed periods**: the refusal is routed to a manual lane with the invoice date and the proposed posting date; the next open period is a human choice.

**Prompt injection**: text inside an invoice is data. It never reaches a model that has tools, and no model output is interpreted as an instruction.

## 5. Learning

**History sources, best first.** (a) Fortnox supplier invoices: the spec documents `SupplierNumber`, the booked `Account`, `CostCenter` and `Project` on each row, and `VoucherSeries/Number/Year` linking the voucher. (b) Vouchers via the verified cache (ADR-0006). (c) A SIE4 export. All through the existing read-only client (ADR-0001: note that Fortnox scopes cannot be read-only, so the client-side gate is the only control and any new client must be covered by the wiring test).

**Method.** Retrieval over the tenant's own bookings. Rank accounts by similarity-weighted votes over resolved supplier, then line description, amount band, VAT rate and currency; a supplier's `PreDefinedAccount` in the ERP is a prior. Unseen supplier with no evidence: no proposal.

**Corrections are first-class, append-only records**: proposal id, document hash, what was proposed, what the human chose, who, when, extractor/model/rule versions. They feed the next proposal and later the evaluation set.

**Backtest on day one.** The time-split held-out harness runs on the imported history before the first live invoice. A supplier's accuracy is reported as a lower confidence bound, not a point estimate, with a recency weight; a supplier below a minimum sample stays in review.

**Confidence is calibrated, not asserted**: observed accuracy per score band per tenant on held-out data, recomputed as corrections arrive.

## 6. Output ladder and the ADR conflicts

| Rung | Output | Writes a ledger? | Gate |
|---|---|---|---|
| L0 | on-screen proposal, CSV | no | this design |
| L1 | SIE4 file the user imports | no (the user imports it) | import test; accounts must exist in the target chart |
| L2 | unbooked supplier-invoice draft in the ERP, test company only | yes, unbooked | **successor ADR to ADR-0001** (#65), the write-gate assertion (#109), wiring test covering the new sink, tier-4 approval (ADR-0012) |
| L3 | booked entries in a live company | yes, corrected only | successor ADR, propose-confirm handle, approval bound to the artifact hash (ADR-0007) |

Only L0 and L1 are in scope of this decision.

**ADR-0002 amendment needed.** Its port is `Parse(source, io.Reader) ([]Transaction, error)` and the code (`internal/statements`) is a bank-statement parser; its idempotency key is statement-shaped. Invoices need a new `Document` type and port with per-field provenance. ADR-0002 also calls PDF parsing a last resort; this design makes the vision model a core stage, bounded by the two-source rule (principle 5) and layout-drift detection through that disagreement rate.

**Reuse, do not duplicate**: `internal/receipts` (mail, IMAP, PDF, a Fortnox write client), `internal/domain` ledger ports (`SupplierLedger`, `GeneralLedger` with chart of accounts, cost-centre and project ledgers, `VoucherCache`), `internal/adapter/fortnox/erp_writer.go` (`domain.ERPWriter`) for any later sink.

## 7. Tenancy and data (deferred until storage exists)

`TenantID` stays first-class (ADR-0009). Per tenant: own ERP integration (ADR-0005), own history, own corrections, own thresholds; `TenantID` enforced at the retrieval and sink ports and on any embedding index or cache. The demo stores nothing. A product needs retention, encryption at rest, deletion on request and an access log (invoices are personal data for sole traders); that is not designed here. Cross-tenant learning is out of scope until a privacy review: sharing only supplier-to-category priors keyed by public identifiers as counts needing k tenants is the candidate, with an opt-in.

## 8. Evidence (what is known, and how well)

| Claim | Status | Source |
|---|---|---|
| Extraction 99.3% of labelled fields on 74 real-layout public invoices (qwen2.5vl:7b on iguana); one miss was a dataset label error | measured, synthetic/public data | `docs/research/2026-11-24-sample-datasets.md` |
| Booking proposal top-1 96.6% (28 of 29) held-out | measured, **synthetic history with 8% built-in noise, one small sample: not a calibration** | same |
| Entry builder: 20,000-case property test and a 14M-execution fuzz found no unbalanced entry after an overflow was fixed | measured | PR #151 review record |
| Fortnox supplier invoice carries supplier number, row accounts and the voucher link; supplier carries organisation number, VAT number, bank account, predefined account | **documented in the vendored spec, not verified on live data** | `docs/vendor/fortnox-openapi.json` |
| Voucher descriptions carry the supplier name | **unverified** (decides whether voucher text can be used at all) | spike 1 |
| Text layer beats the vision model on real Swedish PDFs | **unverified** | spike 2 |
| Any accuracy on real customer history or real Swedish scans | **not measured; bet #142 outcome (a) is unmet** | |

## 9. Parameters (each needs a default, an owner and a test before it is used)

| Parameter | Default | Owner | Set from |
|---|---|---|---|
| Minimum past bookings for a supplier to be eligible for the pre-ticked lane | none yet | tenant owner | day-one backtest |
| Minimum lower-bound accuracy for that lane | none yet | tenant owner | day-one backtest |
| Shadow period before any pre-ticked lane for a new tenant | none yet | tenant owner | pilot |
| Amount ceiling | 1e12 minor units (exists) | engineering | code |
| Rounding tolerance | 0.50 (exists) | engineering | Swedish öresavrundning |
| Drift alert | none yet | engineering | after the first real backtest |
| Retention of documents and corrections | none yet | tenant owner | before any storage |

## 10. Scope and order

**Now (Friday 2026-11-27):** freeze the demo on the current path (done and live). Nothing unmeasured is added.

**Next, in this order:**
1. **Read-only Fortnox probe on the company's own books** (answers Q1 and Q2): a read-only in-cluster Job that prints aggregates only (field presence for supplier and supplier invoice; how many supplier invoices have a linked voucher; the day-one held-out accuracy). Customer ledger lines never enter an agent's context.
2. **Text layer versus vision on 10 or more real Swedish invoices, redacted, run in-cluster** (Q3). Build a deterministic text-layer extractor only if the vision model falls short.
3. **SupplierResolver on whatever identifier the probe shows is present, plus the corrections record and the duplicate key.**
4. **Rules as validator** for domestic VAT only (25/12/6/0); refuse reverse charge and import explicitly. Add the line model, credit notes and per-rate checks.
5. **L1 SIE4 export** with import tests.

**Later, kept as direction only:** standing approval rules (a bounded, expiring, revocable rule signed once; needs ADR-0007 extended); known-answer canaries in review to measure whether reviewers look; a shared supplier-category prior (needs the privacy review); embeddings for cold start; L2 and L3; calibrated confidence dashboards and drift alerts; the build-versus-consume market question (needs real market research and ADR-0009's pause lifted).

## 11. Review record

| Lens | Verdict | Taken | Discarded or deferred |
|---|---|---|---|
| Design critic | Stage order wrong; missing segmentation, doc type, credit notes, mixed rates, duplicates; ADR-0001/0002 conflicts | all, sections 3 to 6 | none |
| Scope (zoom-out) | Over-built for one tenant on a pause; cut ports and calibration | scope and order in section 10; "Later" list | its "packages absent on main" remark: **my error, it read a stale checkout 41 commits behind**; the cut of the embedding and calibration work is taken as deferral, not deletion |
| Contradictions | Seven false compromises with transforms | backtest on day one and per-supplier lanes; rules as validator; upstream extraction as second source; evidence citation precondition | standing rules, canaries and the shared prior deferred as costly or needing review |
| Readiness | Undefined thresholds, no acceptance criteria, unresolved unknowns | parameters table; spikes first; data shapes named | its "Today column unverifiable" blocker: **same stale-checkout error**; per-stage given/when/then and golden fixtures are still to write |
| Code accuracy | ADR-0002 signature wrong; reuse missed | reuse list in section 6; port amendment; float64 note | its "package absent" verdicts: **same stale-checkout error** |
| Cold read | Terms undefined, ask unclear, two tensions | sections 0 and 1; consume-versus-build and local-models boundaries now stated | none |

Three of six reviewers (scope, readiness, code accuracy) read a checkout that was 41 commits behind `main` because I pointed them at the shared working copy without updating it. The accuracy claims in this document were re-checked against `origin/main` (`c681fec`) by me, not by them.
