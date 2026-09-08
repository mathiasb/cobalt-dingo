# Architecture Decision Records — cobalt-dingo

The live ADR series for this repo, numbered sequentially from `0001`.

`../../DECISIONS.md` is **frozen legacy**. It holds fifteen dated entries from
2026-04-08 to 2026-04-27 in a bespoke format with no frontmatter, no status
field and no validation section. Nothing has been appended to it since, and
decisions taken after that date went either here or to `mathias/infra`. It is
kept because those entries are still the reasoning of record for the choices
they describe — Go+HTMX, Task over Make, the PISP-partner boundary, the
ERP-agnostic core. Do not add to it.

## Which repo does a decision belong in?

| Scope | Home |
|---|---|
| How cobalt-dingo works internally | **here**, `docs/adr/` |
| Estate-wide, or spanning repos | `mathias/infra`, `docs/decisions/` |

`ADR-0021` (coo-agent consumed by cobalt-dingo) lives in infra despite being
about this repo, because it decided the fate of *two* repos and a deployment.
The read-only boundary in `0001` below is purely this codebase's, so it lives
here.

## Referring to these

Within this repo the bare number is fine (`ADR-0001`). Across repos, qualify
it — `cobalt-dingo#0001` — because `infra`, `cad`, `swedsl`, `brain-gardener`,
`openbanking-api` and `agentsquad` each keep an independently-numbered series
and a bare `ADR-0001` is ambiguous estate-wide.

## Every ADR here carries an executable assertion

Per `infra` ADR-0020, the `verify:` field in the frontmatter names a command
that asserts the decision's **end state** and exits non-zero when the decision
is no longer in force. It asserts the outcome, never the mechanism: an
assertion that would have passed on the day the ADR was written, with nothing
yet built, is the wrong assertion.

| # | Title | Status | Verify |
|---|---|---|---|
| [0001](0001-read-only-live-financial-data.md) | Live company data is read-only; the write path stays sandbox-only | accepted | `scripts/assert-live-readonly.sh` |
| [0002](0002-ingest-files-not-vendor-integrations.md) | Financial sources arrive as parsed files on an ingest channel, not as per-vendor API clients | accepted | `scripts/assert-no-vendor-clients.sh` |

Both were accepted by Mathias on 2026-09-08, each with a stated direction of
travel recorded in its Status section: 0001 anticipates controlled *reversible*
writes once the system is proven (successor pre-filed as issue #65), and 0002 is
accepted **as a start**, with real APIs and MCP servers as the destination once
business value is demonstrated.

Neither of those intents is a decision yet. Both are recorded where the next
reader will find them rather than left in a chat log.
