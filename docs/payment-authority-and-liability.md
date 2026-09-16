# Payment authority, evidence and liability

**Status:** Draft for decision. Nothing here is decided until the marked
`DECISION NEEDED` blocks are answered and the result is lifted into an ADR.
**Written:** 2026-09-16.
**Closes:** the deliverable of [#106](https://git.d-ma.be/mathias/cobalt-dingo/issues/106),
and is the artifact that makes [#20](https://git.d-ma.be/mathias/cobalt-dingo/issues/20)
answerable rather than a phone call we keep not making.
**Constrains:** #65 ("proven enough" for controlled writes), #88 (production-safety
review), #105 (actor attribution), #46 (multi-tenant SaaS), #62 (FX reconciliation).

---

## 0. Why this document exists, and what it is not

`ADR-0001` makes cobalt-dingo read-only against live books. Everything shipped to
v0.64.0 lives inside that boundary. The half of the product that is the actual
differentiator — money leaving a bank account because software decided it should —
is unbuilt, and it is unbuilt for a good reason: **we have never written down who is
allowed to cause that, on what evidence, or who pays when it goes wrong.**

`docs/irreversible-operations.md` (2026-09-14) is the nearest thing we have and it is
right about everything it covers. But its scope is irreversibility **in the books**:
posted vouchers, consumed voucher numbers, closed periods. This document covers
irreversibility **in the money**, which is harder, because the counterparty is a bank
and the correction path is not a *rättelseverifikation* but a recovery request.

**This is not a component architecture.** It deliberately says almost nothing about
packages or types until §8. The order is intentional: the authority model determines
the domain model, not the other way round, and writing them the other way round is how
a system ends up with an approval flow bolted onto a pipeline that was designed without
one.

### Success criterion for this document

It is done when:

1. each of the six questions a payment platform will ask (§9) has either a stated
   position or a `DECISION NEEDED` with a named owner;
2. each failure mode in §3 has a named bearer of loss, or an explicit statement that
   it is unresolved and why;
3. the evidence model (§4) names what is retained, where, and for how long;
4. §8's consequences are written so they can be lifted into `docs/adr/0007-*.md`
   without rewriting.

### Assumptions, stated rather than buried

- Sweden-domiciled customers, SEK-denominated books, foreign-currency payables. Not
  yet a cross-border-establishment question.
- The payer is a business. No consumer payments, so consumer-protection regimes are
  out of scope — but note §7, which is *not* the same as saying SCA is out of scope.
- We are not, and do not intend to become, a licensed payment institution.
- Regulatory research cited here is the estate's own, dated 2026-09-06, and carries
  its authors' own confidence markers. Where a source says "well-supported, not
  settled", this document repeats that rather than laundering it into certainty.

---

## 1. The authority chain

Money moves only at the end of an unbroken chain. Every link must be answerable
afterwards from stored data, not from memory or from a log line.

```
  Supplier invoice exists in Fortnox
        │  (evidence: Fortnox invoice id + fetched payload)
        ▼
  Beneficiary details resolved            ← the quiet failure point (§3, R-PAY-03)
        │  (evidence: supplier record snapshot, source, fetch time)
        ▼
  Batch assembled                         ← deterministic, byte-stable
        │  (evidence: exact bytes + content hash)
        ▼
  Human reviews and approves THIS batch   ← the authority event
        │  (evidence: actor, timestamp, hash of what was shown, method)
        ▼
  Batch submitted through a channel       ← §6: a port, not a partner
        │  (evidence: channel, credential identity, submission response)
        ▼
  Bank executes                           ← irreversible from here
        │  (evidence: camt.05x confirmation, actual rate, actual fees)
        ▼
  Write-back to Fortnox                   ← books catch up with reality
```

Two properties this chain must have, and they are the whole design:

- **Every link stores its own evidence at the time it happens.** Reconstructing "what
  did the approver see?" from current data is not evidence; the supplier's IBAN may
  have changed since.
- **The approval binds to a specific artifact, not to an intent.** "Approve the
  September batch" is not an authorisation. "Approve *these bytes*, whose hash is X"
  is. This is why `internal/payment/pain001.go`'s determinism is not a nicety — a
  batch that regenerates differently cannot be the thing that was approved.

---

## 2. Actors and what each may cause

| Actor | May cause | May never cause |
|---|---|---|
| **Payer** (customer's authorised signatory) | Approval of a specific batch; the only actor whose act authorises money movement | — |
| **Consultant** (accounting firm, acting for the payer) | Everything up to approval: assemble, review, correct, resubmit for approval | Approval, unless the payer has delegated it in a recorded, revocable way |
| **cobalt-dingo the system** | Detection, enrichment, assembly, submission *of an already-approved artifact* | Approval. Ever. Under any configuration |
| **An agent** (MCP client, LLM) | Everything the consultant may do, plus proposing | Approval, and any write whose preview it did not show |
| **Operator** (us) | Infrastructure, support, incident response | Any act inside a tenant's authority chain |

The line is in one place: **approval is a human act by a person the payer's bank
mandate recognises, and nothing else in the system may synthesise it.** Everything
else — detection, enrichment, assembly, even submission — is mechanical and may be
automated, because none of it moves money on its own.

This is stricter than accounted's equivalent and deliberately so. They removed agent
auto-commit for *bookkeeping* (`#394`, migration `20260505190027_drop_agent_auto_commit`)
and were right to. Our irreversibility is worse: a wrong voucher is corrected, a wrong
payment is chased.

---

## 3. What can go wrong, and who bears it

Four failure modes. The liability column is the part that is not yet decided; where it
says **UNRESOLVED** that is a real gap, not a placeholder for prose.

### 3.1 Correct instruction, wrong beneficiary

The instruction is exactly what the payer approved; the beneficiary account is wrong.
Sources: a poisoned supplier record in Fortnox, an IBAN changed by an attacker
(invoice-fraud / BEC), a legitimate change we cached before it was verified.

`docs/irreversible-operations.md` already calls supplier bank-detail edits Tier 3 —
"quiet, and therefore arguably worse" — and that is exactly right here: **we would be
faithfully executing a corrupted instruction, and the corruption entered upstream of
us.**

Liability: **UNRESOLVED.** Note that it is about to be partly regulated, which is the
single most useful timing fact in this document:

- **Verification of Payee (VOP)** is mandated by the **Instant Payments Regulation**,
  not by PSD3/PSR. Eurozone PSPs were reachable by 2025-10-09; **non-euro EEA, which
  includes Sweden, is 2027-07-09.**
- The **PSR separately** introduces a payee-name/IBAN matching **liability regime for
  ordinary (non-instant) credit transfers**, on its own later clock (~24 months post
  entry into force).

These are two legally distinct obligations with different deadlines and must not be
conflated in anything client-facing. (Estate research, 2026-09-06:
`wiki/openbanking/facts/premium-apis-vop-and-fida-treasury-drift-signals-to-watch`.)

**What this means for us now:** a payee-verification step is going to exist in this
corridor regardless of what we decide. Designing the evidence model so a
verification result can be recorded against a beneficiary *before* it is required is
cheap now and expensive to retrofit.

### 3.2 Duplicate execution

The same batch reaches the bank twice: a retry after an ambiguous timeout, a resubmit
after a UI error, two operators acting at once.

Liability: **ours, and unambiguously so**, because duplicate suppression is entirely
within our control and the primitive already exists. Banks dedupe on the message
identifier; our generator must therefore produce a stable `MsgId` for a stored batch
and a different one for a genuinely new batch. Harvested from a bank-validated
implementation (`knowledge/swedish-domestic-pain001-wire-constraints`): *"`CreDtTm`
comes from the caller (the batch row's `created_at`), never from the clock, so
re-generating a stored batch is byte-identical and bank-side duplicate detection
stays meaningful."*

This is the one failure mode where the mitigation is already designed and merely needs
to be true in our code and tested. See R-PAY-02.

### 3.3 Paying an invoice already settled elsewhere

The payer, or their bank, or another tool, settled the invoice outside cobalt-dingo
between our read and our submission. We pay it again.

Liability: shared and situational; recovery is commercial (ask the supplier to return
it), not technical. The mitigation is freshness, not cleverness: re-read settlement
state immediately before submission and refuse on drift, rather than trusting the
state captured at assembly time. Cheap; must be explicit.

### 3.4 FX applied at execution differs from the FX previewed

The payer approved a batch showing an indicative SEK cost; the bank executes at its
own rate, plus fees. The books must then reflect reality, not the preview.

Liability: **not a liability event at all if the preview is labelled honestly**, and a
dispute waiting to happen if it is not. The design rule is that a preview must state
it is indicative, name its rate source and timestamp, and never be presented as the
amount that will be debited.

Estate fact, already learned: reconciliation between the instructed amount and the
settled amount **always** needs a manual exception lane for bank fees and FX gain/loss
(`knowledge/ap-automation-cannot-auto-reconcile-fx-fees-and-gainloss`). This is the
same conclusion #62 reached from the other direction; the two should be built as one
thing.

---

## 4. The evidence model

What must survive, so that "the payer authorised this" is provable a year later by
someone who was not there.

| Artifact | Captured when | Why it must be stored, not derived |
|---|---|---|
| Source invoice snapshot | At detection | Fortnox is mutable; the invoice may be edited or settled later |
| Beneficiary snapshot + source + fetch time | At enrichment | The supplier's IBAN today is not evidence of the IBAN we paid |
| Payee-verification result, when available | At enrichment | §3.1; a dated obligation is coming, and the record is the defence |
| **Exact batch bytes + content hash** | At assembly | This is the thing approved. Regenerating it is not the same as storing it |
| **Approval record**: actor identity, timestamp, hash of what was displayed, method (and, when applicable, the bank's own signature reference) | At approval | The authority event. Without the displayed-artifact hash, "approved" is an unfalsifiable claim |
| Submission record: channel, credential identity, request id, response | At submission | Distinguishes "we sent it" from "they accepted it" — the gap where duplicates are born |
| Execution confirmation (camt.05x), actual rate, actual fees | At execution | Reconciliation, and the FX/fee exception lane |

Retention: the generated payment file is *underlag* for the payments it initiates and
is therefore **räkenskapsinformation under BFL 7 kap — seven years**. The approval
record is what makes the underlag meaningful and inherits the same period. This is not
a product choice; treat the retention as fixed and design deletion paths around it.

Attribution: every one of these rows carries the acting identity, set once at the API
boundary and stamped onto the persisted row — #105. Without it, the evidence model
above is a collection of undated assertions.

---

## 5. The authority model: what "approved" means mechanically

Three candidate models. They are not equivalent in risk and should not be a
configuration afterthought.

| Model | What it is | Risk profile |
|---|---|---|
| **A. Single approval per batch** | One authorised human approves the assembled batch as a unit | Simplest; the blast radius is the whole batch, which is exactly what the payer sees and approves |
| **B. Approval per payment** | Each payment approved individually | Lowest blast radius per act, highest friction; realistically means bulk-approving, which re-creates A while feeling safer |
| **C. Dual authorisation above a threshold** | A second authorised human above a configured amount | Matches how corporate treasury actually works, and is what a finance function will expect to see |

> **DECISION NEEDED — authority model**
> **Question:** Which model ships first, and is dual authorisation a v1 feature or a
> configurable we design the schema for and leave off?
> **Options:** (A) single-per-batch only; (B) per-payment; (C) A plus threshold-based
> dual authorisation.
> **Recommendation:** **A now, with the schema shaped for C.** The approval record in
> §4 should carry a set of approvals rather than one, so adding a second approver later
> is a policy change and not a migration of evidence. B is friction that buys nothing
> we do not get from A plus a good preview.
> **Blocks:** #20 (a platform will ask), #65, #88.

A caution against locking this to any regulatory assumption: the PSR "clarifies the
treatment of SCA for business-to-business payment flows" and is described as a
significant departure from PSD2's ambiguity — but **the actual mechanics are not
public** in any source the estate has found, and current corporate SCA exemptions
should not be assumed to carry over (research 2026-09-06). Therefore: **the signature
and step-up policy must be a named, replaceable policy object, not conditionals
scattered through the submission path.**

---

## 6. The submission channel is a port, not a partner

This is the most consequential reframing in this document, and it changes what #20 is
asking.

#20 states the assumption as existential: *"If no suitable partner exists at
early-stage scale without enterprise contracts, the payment pipeline cannot be
built."* On the evidence, that is too strong. There are **three** channels by which an
approved batch reaches a bank, and only one of them needs a PISP relationship:

| Channel | How it reaches the bank | Our regulatory position | Status |
|---|---|---|---|
| **(a) File export** | We produce the file; the payer uploads it in their own corporate bank portal, under their own bank agreement | Outside the TPP perimeter entirely. We never touch a payment account | **Built today** — the signed XML download |
| **(b) PISP platform** | We submit through a licensed platform (Tink / Aiia / Token.io) that initiates on the payer's behalf | The platform is the regulated entity; we are its technology customer. Matches DECISIONS.md 2026-04-15 | Not started — #20 |
| **(c) Corporate host-to-host** | The payer's own bilateral corporate channel, under their ordinary banking-services agreement | Structurally outside PSD2/PSR's AISP/PISP regime, because no third-party access right is being exercised | Not started |

On (c), the estate's own research is explicit about its confidence, and this document
repeats the caveat rather than smoothing it: *"no EBA, Deloitte, or industry-body
source found states this boundary as an explicit statutory carve-out. It is a
structural inference ... treat it as 'very likely true, worth confirming per-bank'
rather than 'settled law.'"* (`wiki/openbanking/facts/corporate-treasury-apis-sit-outside-psd2-psr-by-structure-not-explicit-carveout`, 2026-09-06.)

**Three consequences:**

1. **#20 is no longer existential; it is a channel-quality question.** The honest
   framing is: (a) already works and is how the incumbent operates — `erp-mafia/accounted`
   produces a domestic payment file and the human uploads it, with no PISP anywhere in
   their tree. (b) buys one-click execution and is the difference between a tool and a
   product. A "no" from every platform costs us the click, not the pipeline.
2. **The domain needs a `PaymentSubmitter` port with three adapters**, of which one
   exists. `internal/domain/ports.go` already declares `PaymentSubmitter`; this is
   confirmation that the existing shape is right, not a new abstraction.
3. **The regulatory posture is per-channel, not global.** Any statement of the form
   "cobalt-dingo is/is not in scope of PSD2" is wrong unless it names the channel.

> **DECISION NEEDED — channel sequencing**
> **Question:** Do we harden (a) into a supported, evidenced product path before
> pursuing (b)?
> **Options:** (1) harden (a) first, pursue (b) in parallel as a commercial
> conversation; (2) treat (b) as a prerequisite and leave (a) as the dev artifact it is
> today; (3) pursue (c) with a specific bank via Jonas's relationships.
> **Recommendation:** **(1).** It makes the authority, evidence and reconciliation
> model real against actual money at the lowest regulatory exposure, and every artifact
> it produces is exactly what (b) needs anyway. It also converts #20 from a blocker
> into a roadmap item, which is the honest description once (a) works.
> **Blocks:** #20, #22 (a file-export product and a one-click product are not the same
> price), #88.

---

## 7. Regulatory perimeter and the dated obligations

Where we sit today, per channel, and what is coming with a date attached.

**Today, under PSD2 as applied:** channels (a) and (c) do not exercise third-party
access rights and do not make us an AISP or a PISP. Channel (b) places the licensed
obligations on the platform; our exposure is contractual, through them, not regulatory,
through a licence we do not hold. The estate has an equivalent boundary written down
for `openbanking-api` — ADR-0001 there, "observe and cite, never call a bank" — and the
reasoning transfers.

**Dated, and worth designing toward now:**

| What | Who it binds | Date | Our exposure |
|---|---|---|---|
| **VOP** under the Instant Payments Regulation | PSPs | **2027-07-09** for non-euro EEA (Sweden) | Not binding on us. But a payee-check step will exist in the corridor, and our evidence model should be able to record its result |
| **PSR payee-name/IBAN matching liability regime** for ordinary credit transfers | PSPs | ~24 months post entry into force, separate clock | Shapes §3.1's answer; do not conflate with VOP |
| **PSD3 / PSR** generally | The regime | Genuinely unresolved; **plan 2027–2028, not a fixed date** | Design against PSD2 today, monitor |
| **SCA for B2B flows** under PSR | Us, via channel choice | Mechanics not public | §5's policy object exists precisely because of this |
| **FIDA** | Broader data access | In trilogue as of 2026-04, own clock | Out of scope now; the plausible long-term vehicle for regulated business data sharing |

All five are estate research dated 2026-09-06 with sources; **verify against EUR-Lex
before quoting any of these dates to a client or a platform.**

---

## 8. Invariants, as acceptance criteria

EARS-lite, so each line is a test rather than a sentiment. These hold on every channel.

```
THE SYSTEM SHALL require a human approval record before any batch is submitted
THE SYSTEM SHALL bind each approval to the content hash of the exact artifact displayed
THE SYSTEM SHALL regenerate a stored batch byte-identically for the same generator version
THE SYSTEM SHALL stamp the acting identity on every row in the authority chain
THE SYSTEM SHALL retain the batch, the approval record and the execution confirmation
  for seven years

WHEN a batch is submitted more than once
  THE SYSTEM SHALL reuse the stored message identifier so the bank's duplicate
  detection can act

WHEN beneficiary details have changed since the approval record was created
  THE SYSTEM SHALL refuse submission and require re-approval

WHEN an invoice's settlement state has changed since assembly
  THE SYSTEM SHALL refuse to submit that payment and report which one

WHEN an approval is older than the configured freshness window
  THE SYSTEM SHALL refuse submission and require re-approval

WHILE any payment in a batch lacks a resolvable beneficiary
  THE SYSTEM SHALL refuse to assemble the batch rather than silently omitting it

IF the executed amount, rate or fees differ from the approved preview
  THEN THE SYSTEM SHALL record the difference and route it to the manual exception
  lane, never adjusting the books silently

IF an agent attempts to approve
  THEN THE SYSTEM SHALL refuse, regardless of configuration or scope
```

The last one deserves emphasis: it is not a permission check to be granted later. It
is the property that makes everything above meaningful.

---

## 9. What a payment platform will ask on the first call

The #20 call script. Our current answer, and where it is missing.

| # | Their question | Our answer today |
|---|---|---|
| 1 | Tech provider, or agent of a payment institution? | Technology provider; the platform holds the licences (DECISIONS.md, 2026-04-15). Stated, never tested with a counterparty |
| 2 | Who initiates, and what evidence proves the payer authorised it? | §1 and §4 — **as of this document**. Not yet implemented |
| 3 | SCA / signature model? | §5, recommendation A-shaped-for-C. **Needs your decision** |
| 4 | What prevents wrong or duplicate submission? | §3.2 duplicate key (designed, must be tested); §3.1 beneficiary integrity **unresolved** |
| 5 | Who bears loss on a misdirected payment? | **Unresolved** — §3.1, and the regime is moving |
| 6 | Volumes, currencies, corridors? | **Unknown** — depends on #22, which is also open |

Questions 3, 5 and 6 are ours to answer before the call is worth making. That is the
whole argument for writing this document before dialling.

---

## 10. Consequences for the codebase

Written as ADR-able statements, so they lift into `docs/adr/0007-*.md` unchanged once
decided. None of these are implemented today.

1. **`Approval` is a domain concept**, not a UI state: `{approvers[], approved_at,
   displayed_artifact_hash, method, batch_id}`. A set, not a single approver, so
   dual authorisation is later policy rather than a migration of evidence.
2. **`Batch` gains an immutable approved artifact**: the bytes and their hash, stored
   at approval. `BatchStatus` gains the transition that cannot be reversed.
3. **`PaymentSubmitter` grows adapters, not branches.** File export is an adapter, not
   the absence of one. Regulatory posture is a property of the adapter.
4. **Beneficiary resolution stores a snapshot with provenance**, and a change between
   approval and submission is a refusal, not a silent re-resolve.
5. **A `SignaturePolicy` object** owns thresholds and step-up, because §7 says the
   rules under PSR are not yet knowable.
6. **Actor attribution at the API boundary** (#105) is a prerequisite for all of the
   above, not a parallel nicety.
7. **The FX/fee exception lane is part of the payment path**, not a reporting
   afterthought — same conclusion as #62, reached from liability rather than from
   reconciliation.
8. **Approval is unreachable from the MCP surface.** Not "requires a scope" —
   absent from the tool catalogue entirely. Compare #45, where staging is the pattern
   for bookkeeping writes; here staging is the *only* thing an agent may do.

---

## 11. What this document deliberately does not decide

- The component architecture of the payment path. That follows the decisions above,
  and writing it first would be inventing a partner's contract (§6).
- Pricing or buyer (#22), except to note in §6 that the channel choice changes both.
- Whether cobalt-dingo ever becomes a regulated entity. Current answer is no, and
  nothing here assumes a future yes.
- The contractual wording. This document states what the contract must cover; a lawyer
  states how. **That is a real dependency and it has no owner** — see #106.

---

## See also

- `docs/irreversible-operations.md` — irreversibility in the books; this document's sibling
- `docs/adr/0001-read-only-live-financial-data.md` — the boundary this work would eventually cross
- `docs/risk-register.md` — the R-PAY entries derived from §3
- `DECISIONS.md` 2026-04-15 — the tech-provider / PISP-partner split, frozen legacy
- Brain: `wiki/openbanking/facts/corporate-treasury-apis-sit-outside-psd2-psr-by-structure-not-explicit-carveout`,
  `wiki/openbanking/facts/premium-apis-vop-and-fida-treasury-drift-signals-to-watch`,
  `wiki/openbanking/facts/psd3-psr-timeline-uncertain-application-2027-to-2028`,
  `knowledge/ap-automation-cannot-auto-reconcile-fx-fees-and-gainloss`,
  `knowledge/swedish-domestic-pain001-wire-constraints`
- `~/dev/docs/research/erp-mafia-accounted-blueprint.md` — how the nearest competitor
  answered the same questions, and where they declined to
