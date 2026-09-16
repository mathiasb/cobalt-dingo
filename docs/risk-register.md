# Risk Register

All entries follow the `R-[DOMAIN]-[NN]` schema. See the `regulatory-risk-assessment`
skill for conventions.

Scope of this first pass: the **payment path** — the authority chain from a detected
supplier invoice to money leaving a bank account, as designed in
[`payment-authority-and-liability.md`](payment-authority-and-liability.md). Risks in
the read-only surface shipped up to v0.64.0 are not re-assessed here; `ADR-0001` and
`docs/irreversible-operations.md` cover that boundary.

**Overall risk level for the payment path: HIGH.** Driven by R-PAY-01 and R-AGENT-01.
Nothing in this register is mitigated today, because none of the payment path is built —
which is the correct state, and the reason `ADR-0001` holds.

Last updated: 2026-09-16.

---

### R-PAY-01 — A correct instruction pays the wrong beneficiary

| Field | Value |
|-------|-------|
| Risk | The batch faithfully executes what was approved, but the beneficiary account is wrong: a supplier record poisoned upstream in Fortnox, an IBAN substituted by invoice fraud (BEC), or a legitimate change cached before verification. Money reaches a stranger and recovery is a commercial chase, not a reversal |
| Regulatory reference | Verification of Payee under the Instant Payments Regulation — **non-euro EEA (Sweden) 2027-07-09**; separately, the PSR payee-name/IBAN matching liability regime for ordinary credit transfers on its own later clock. Two distinct obligations, different deadlines |
| Likelihood | M — supplier-invoice fraud is the most common attack on exactly this workflow, and `docs/irreversible-operations.md` already classes supplier bank-detail edits as Tier 3, "quiet and therefore arguably worse" |
| Impact | H |
| Overall | **H** |
| Mitigation | Beneficiary snapshot with provenance stored at enrichment; a change between approval and submission is a refusal, not a silent re-resolve; evidence model shaped to record a payee-verification result before one is required. Liability allocation itself is **unresolved** — that is #106, not a control |
| Validation | `TestSubmit_RefusesWhenBeneficiaryChangedSinceApproval`; `TestEnrich_StoresBeneficiaryProvenance`; #106 for the liability position |
| Status | open |

---

### R-PAY-02 — The same batch executes twice

| Field | Value |
|-------|-------|
| Risk | A retry after an ambiguous timeout, a resubmit after a UI error, or two operators acting at once sends the same batch to the bank twice. Every payment in it duplicates |
| Regulatory reference | None directly; the control is the bank's own duplicate detection, which is keyed on the message identifier we supply |
| Likelihood | M — ambiguous timeouts are normal, and the obvious human repair is to press the button again |
| Impact | H |
| Overall | **H** |
| Mitigation | `MsgId` and `CreDtTm` derive from the stored batch row, never from the clock, so a stored batch regenerates byte-identically and the bank can dedupe. A genuinely new batch must produce a different identifier. Submission records distinguish "sent" from "accepted" |
| Validation | `TestPain001_RegeneratesByteIdentically`; `TestPain001_CreDtTmComesFromBatchNotClock`; `TestSubmit_ReusesStoredMessageIdOnRetry` |
| Status | open |

---

### R-PAY-03 — An invoice already settled elsewhere is paid again

| Field | Value |
|-------|-------|
| Risk | The payer, their bank, or another tool settles the invoice between our read and our submission. We pay it a second time |
| Regulatory reference | None |
| Likelihood | M — cobalt-dingo is not the only actor on the customer's books, by design |
| Impact | M — recoverable commercially from a known counterparty, unlike R-PAY-01 |
| Overall | **M** |
| Mitigation | Re-read settlement state immediately before submission and refuse on drift, rather than trusting the state captured at assembly time |
| Validation | `TestSubmit_RefusesWhenInvoiceSettledSinceAssembly` |
| Status | open |

---

### R-PAY-04 — Executed FX and fees diverge from the approved preview

| Field | Value |
|-------|-------|
| Risk | The payer approves a batch showing an indicative SEK cost; the bank executes at its own rate plus fees. If the preview was presented as the debited amount, this is a dispute; if the books are adjusted silently, it is an audit finding |
| Regulatory reference | BFL — the resulting vouchers are räkenskapsinformation and must reflect what happened, not what was previewed |
| Likelihood | H — near-certain. Rates move and fees exist |
| Impact | L — provided the preview is labelled honestly and the difference is routed, not absorbed |
| Overall | **M** |
| Mitigation | Previews state that they are indicative, naming rate source and timestamp; the difference between instructed and settled amounts routes to a manual exception lane. Estate fact: FX AP reconciliation **always** needs a manual exception path for fees and gain/loss — this is the same conclusion #62 reached from the reconciliation side |
| Validation | `TestPreview_LabelsRateSourceAndTimestamp`; `TestReconcile_RoutesFxAndFeeDeltaToExceptionLane`; #62 |
| Status | open |

---

### R-AGENT-01 — An agent causes money to move

| Field | Value |
|-------|-------|
| Risk | An MCP client, or an LLM driving one, synthesises or reaches an approval — through a tool that exists, a scope that is granted, a retry loop, or prompt injection carried in supplier or invoice text that the agent treats as instruction |
| Regulatory reference | None directly. The payer's bank mandate recognises a person; nothing in software can stand in for that |
| Likelihood | L if the tool is absent; M if it exists behind a scope, because scopes get granted |
| Impact | H |
| Overall | **H** |
| Mitigation | Approval is **absent from the MCP tool catalogue entirely** — not gated by a scope. Agents may propose and stage only. All external text (invoice descriptions, supplier names, mail bodies) is treated as data, never as instruction. Compare `erp-mafia/accounted`, which built agent auto-commit for bookkeeping and then removed it (#394, `20260505190027_drop_agent_auto_commit`); our irreversibility is worse than theirs |
| Validation | `TestMCP_ApprovalToolAbsentFromCatalog`; `TestMCP_NoToolCanTransitionBatchToApproved`; #45 for the staging contract |
| Status | open |

---

### R-DATA-01 — "The payer authorised this" cannot be proved afterwards

| Field | Value |
|-------|-------|
| Risk | The approval is recorded as a status change rather than as evidence bound to a specific artifact. A year later there is no way to show what the approver actually saw — and the supplier's IBAN has since changed, so current data proves nothing |
| Regulatory reference | BFL 7 kap — the payment file is *underlag* for the payments it initiates: seven-year retention. The approval record is what makes that underlag meaningful |
| Likelihood | M — this is the default outcome unless designed for, because a boolean is the obvious implementation |
| Impact | H — it is the evidence on which every liability position in #106 rests |
| Overall | **H** |
| Mitigation | Approval binds to the content hash of the exact artifact displayed; actor identity stamped at the API boundary (#105); batch bytes stored at approval rather than regenerated; seven-year retention applied to batch, approval and execution confirmation together |
| Validation | `TestApproval_BindsToDisplayedArtifactHash`; `TestApproval_RecordsActorIdentity`; #105 |
| Status | open |

---

### R-COMP-01 — A dated payee-verification obligation arrives against an undesigned evidence model

| Field | Value |
|-------|-------|
| Risk | VOP and the PSR payee-matching regime land in this corridor on fixed clocks. If the evidence model has nowhere to record a verification result, adding one later means migrating stored evidence rather than writing a new field |
| Regulatory reference | Instant Payments Regulation (VOP): non-euro EEA **2027-07-09**. PSR payee-name/IBAN matching: separate, later clock. PSD3/PSR generally: plan 2027–2028, genuinely unresolved |
| Likelihood | H — dated obligations arrive |
| Impact | L now, M if retrofitted after real batches exist |
| Overall | **M** |
| Mitigation | Evidence model carries an optional payee-verification result from the start, even while nothing populates it. Dates verified against EUR-Lex before being quoted to a client or platform, never from this register |
| Validation | Schema review at the first payment-path PR; re-check the estate's openbanking research each quarter |
| Status | open |

---

### R-COMP-02 — The regulatory posture is asserted globally rather than per channel

| Field | Value |
|-------|-------|
| Risk | Someone states "cobalt-dingo is outside PSD2" without naming the submission channel. File export and corporate host-to-host sit outside the third-party-access regime; a PISP platform integration does not, and the platform's obligations flow to us contractually |
| Regulatory reference | PSD2/PSR AISP/PISP regime. Note the estate's own confidence marker on the corporate-API boundary: a **structural inference corroborated by market practice, not an explicit statutory carve-out** — "very likely true, worth confirming per-bank", not settled law |
| Likelihood | M — the short sentence is the one that ends up in a deck |
| Impact | M |
| Overall | **M** |
| Mitigation | Regulatory posture is a property of the submission adapter, documented per channel in `payment-authority-and-liability.md` §6–7. Any client-facing claim names the channel |
| Validation | #20's written go/no-go must state the position per channel, not once |
| Status | open |
