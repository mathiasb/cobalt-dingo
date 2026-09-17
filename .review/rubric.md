# Review rubric

The versioned rubric the PR reviewer judges a diff against. Machine-read by
`internal/review.ParseRubric`, so the format is load-bearing:

    ## R-<DOMAIN>-<NN> — <critical|high|medium|low|info> — <title>

Prose under a rule is context for the model and is ignored by the parser. A `##`
heading containing an em-dash separator is treated as a rule and validated
strictly; section headings must not contain one.

**Severity lives here, not in the model's answer.** The diff under review is
attacker-controlled text and the model reading it is the thing we are asking to
judge it — if the model assigned severity, a diff could argue its way below the
blocking threshold. The model reports *which rule is violated where*; this file
decides how much that matters.

**Changing this file changes the gate.** The reviewer is built from `main`, so a
PR that edits this rubric is still judged by the rubric on `main` — the loosening
lands only once it is merged, which is the point.

## The answer path

A finding can be *answered* rather than obeyed. Comment on the PR:

    review-accept: R-SEC-01 — test fixture, not a live key

The rule stops blocking, and the acceptance plus its reason stay in the sticky
comment as part of the record. A reason is mandatory: an acceptance without one is
a bypass, not a decision. This exists because a gate with no answer path becomes
either a rubber stamp or something you route around — `erp-mafia/accounted` triaged
a `transaction_method` backfill finding as satisfied-by-design with the reasoning
written down, and that is the behaviour worth copying.

---

## R-SEC-01 — critical — Secret material appears in the diff

An API key, token, password, private key or connection string with credentials
embedded, in source, config, test fixture or committed output. Placeholder values
that cannot authenticate anything are not findings.

## R-SEC-02 — critical — A credential is printed, logged, echoed or transformed

Any path that could put a live secret into output that is persisted: `echo`/`cat`
of a secret, `slog` of a token field, base64 or hex of key material, a secret in a
command's argv. Tool output reaches transcripts and the brain, so a secret printed
once is searchable forever and clearing it means rotating the key.

## R-WRITE-01 — critical — A write against the live ledger

ADR-0001 makes cobalt-dingo read-only against the live books, enforced at four
layers. Any new HTTP verb other than GET against the ERP, any removal of a
read-only guard, any new mutating endpoint on that path.

## R-WRITE-02 — high — An irreversible operation gains no confirmation path

Deletes, overwrites, submissions and external sends that no code path can undo,
arriving without the staging or confirmation step `docs/irreversible-operations.md`
requires for its tier.

## R-AGENT-01 — critical — An agent-reachable tool can approve, or move money

Per ADR-0007 approval is a human act, and per the risk register R-AGENT-01 the
approval capability is absent from the MCP tool catalogue entirely rather than
gated behind a scope. A new MCP tool that transitions a batch to approved, submits
a payment, or widens an existing tool to do so.

## R-AGENT-02 — high — External text is treated as instruction rather than data

Supplier names, invoice descriptions, mail bodies, PR diffs and model output
interpolated into a prompt, a shell command or a template without being confined
to a data position.

## R-DATA-01 — high — Personal or company financial data reaches a cloud model

Invoice-level detail, account balances, counterparty names or statement rows sent
to a non-local endpoint. Local inference via LiteLLM on koala is fine; the cloud
providers behind the same gateway are not, for this data.

## R-MONEY-01 — critical — A monetary amount is handled as a float

`float64` for an amount, arithmetic on a parsed float, or a conversion that routes
through one. Rounding error in a ledger is an audit finding, not a rounding error.

## R-MONEY-02 — high — Money is parsed or rendered without the domain type

Amounts formatted with `fmt.Sprintf` or parsed with `strconv` instead of going
through the domain money type, so the currency and scale travel separately from the
number.

## R-CI-01 — high — A quality gate is weakened

A check suffixed with `|| true`, a test skipped or deleted rather than fixed, a
linter rule disabled, a threshold loosened, a `task check` step removed. #70 is the
precedent: a `|| true` on the vulnerability scan made the gate unable to fail, so
nobody noticed the tool had stopped working at all.

## R-TEST-01 — high — A behaviour change ships with no test that would have failed before

Not "there are tests" — a test that fails on the old code and passes on the new.
A change to a pure function, a branch, a boundary or an error path with no
corresponding assertion.

## R-TEST-02 — medium — A test asserts the mock rather than the behaviour

Green against a fake that was never checked against the real contract. The estate
has paid for this twice: mocks that passed while the live endpoint 404'd, and a
suite that proved the fake server's shape rather than the API's.

## R-MIG-01 — high — A migration is unverifiable or edits a shipped file

A constraint, `NOT NULL`, unique index or backfill added with no test against a
seeded database — it passes trivially against zero rows and proves nothing (#104).
Also: editing a migration that has already been applied anywhere.

## R-ERR-01 — high — An error is swallowed

A discarded error return, a bare `recover`, a branch that logs and continues where
the caller needed to know, `_ =` on something that can fail meaningfully.

## R-ERR-02 — medium — An error crosses a boundary without operation context

Returned naked instead of wrapped as `fmt.Errorf("operation: %w", err)`, so the
message that reaches a log cannot say what was being attempted.

## R-DOC-01 — medium — A recorded decision is contradicted without a new one

A change that goes against an accepted ADR, `DECISIONS.md`, or a risk-register
mitigation, with no ADR superseding it. Contradicting a decision is allowed;
doing it silently is not.

## R-STYLE-01 — low — An exported symbol lacks a doc comment

Exported type, function, method or package with no comment, in a package that
otherwise documents them.
