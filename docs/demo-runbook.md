# Invoice processor demo: runbook

Bet #142 (placed 2026-11-24), slice #147, PR #148. Demo date: Friday 2026-11-27.

## What is running, and where

| Thing | Value |
|---|---|
| Public URL | `https://invoice-demo.d-ma.be/` (TLS from Let's Encrypt, HTTP redirects to HTTPS) |
| Login | HTTP basic auth. Username and password are in 1Password: vault `AgentSecrets`, item `invoice-demo`. The password is generated and has never been written to a file or a chat. |
| Process | systemd **user** service `invoice-demo.service` on koala, listening on `10.0.1.20:8099` (LAN address only, not all interfaces) |
| Started by | `~/invoice-demo/run.sh`: reads the login from 1Password, drops every `OP_*` variable, then `exec`s `~/invoice-demo/bin/demo` |
| Data | `~/invoice-demo-data/` made by `uv run scripts/gen-synthetic-invoices.py ~/invoice-demo-data`: 146 synthetic past bookings and 36 sample invoices. All fictional. |
| Model | `qwen2.5vl:7b` on iguana (`http://iguana:11434`), refused at startup if the URL is not on the local network |
| Proxy | NPM on piguard: proxy host id **26** `invoice-demo.d-ma.be` to `http://10.0.1.20:8099`, certificate id **32**, SSL forced |

## Which build is running

`~/invoice-demo/bin/demo` is built from **`main`** at `5dbabcf` (the merge of PR #148, which carries #151). Rebuild after any later change:

```
cd <a clean checkout of main>
go build -o ~/invoice-demo/bin/demo.new ./cmd/demo && mv ~/invoice-demo/bin/demo.new ~/invoice-demo/bin/demo
systemctl --user restart invoice-demo.service
```

A restart forgets every correction and every downloadable CSV (both are in memory only).

## Converter dependency (PDF and Word)

The demo runs `uvx --quiet --from 'markitdown[docx,pdf]' markitdown` as a subprocess for PDF and Word (first run downloads ~38 packages; `uv` must be on the service's `PATH` and its cache reachable through `HOME`). The child gets only `PATH`, `HOME`, the cache variables and `LANG`, never the login or the 1Password session. Settings: `DEMO_TEXT_MODEL` (default `DEMO_MODEL`), `DEMO_MARKITDOWN` (replace the command). If `uvx` is missing, images and XML still work and a PDF or Word upload fails with the model-busy sentence; check `journalctl --user -u invoice-demo.service` for `converter could not run`.

Samples of every format: copy the generated documents next to the PNGs, named `NNN.pdf`, `NNN.docx`, `NNN.xml` (three digits), into `~/invoice-demo-data/invoices/`. Other names are ignored. Generate with `uv run scripts/gen-variety.py <dir>`; the files are fictional.

## Everyday commands

```
systemctl --user status invoice-demo.service
journalctl --user -u invoice-demo.service -n 50 --no-pager
systemctl --user restart invoice-demo.service
```

## Rotate the password

Edit the `invoice-demo` item in 1Password (new password), then restart the service. Verify by effect: the old password returns 401 and the new one 200. Do not compare values.

## Take it down

1. `systemctl --user disable --now invoice-demo.service`
2. Delete NPM proxy host 26, then certificate 32 (NPM admin UI, or the API as in brain `npm-api-le-cert-empty-meta-quirk`).
3. The 1Password item may stay; delete it if the demo is retired.

Wildcard DNS means there is no record to remove. After step 2 the name falls through to whatever NPM does with an unknown host.

## What it does and does not do

- Reads up to 4 files per submit (and total size capped). Formats, decided from the file's bytes and not its name: PNG and JPEG images (vision model), **PDF with a text layer and Word .docx** (converted to text by `markitdown`, read by the text model), and **e-invoice XML, Peppol BIS Billing 3 / UBL** (parsed by code, no model). A **scanned PDF** is refused with an explanation, never guessed at; CII (XRechnung/ZUGFeRD) is not read yet (#161). Measured on generated data only: `docs/research/2026-11-25-multiformat-extraction-eval.md`.
- Shows the fields, proposes an expense account from past bookings with the vouchers behind it, and learns corrections in memory.
- For each trusted invoice it shows the **proposed accounts payable entry**: debit the expense account, debit input VAT (the account past bookings used, else 2641), credit 2440; a gap up to 0.50 balances on 3740. One CSV of all entries is downloadable for 30 minutes (20 submits at most, memory only). An invoice that does not reconcile, has no proposed account, or is a credit note gets **no entry** and the page says why.
- Entries are in the invoice currency, not converted to SEK, and reverse-charge VAT on foreign purchases is not modelled (the entry says so). See #152.
- **No accounting-system access of any kind.** Nothing is posted anywhere. Uploads are held in memory for one request and are not written to disk (pinned by a test with the temp directory watched).
- One model call at a time and at most 2 uploads being read at once (more get 503). One shared 150 s model deadline per submit. Corrections are capped at 200 until restart.
- The data is synthetic, and the page says so. The accuracy shown says nothing about a real company's books.

## Known limits, tracked in #149 and #152

The learning cap is global; `*.ts.net` model hosts are accepted for any tailnet; the default HTTP transport honours `HTTP_PROXY`; the page has not been viewed in a browser; checks were run from koala, so the path from outside the LAN has not been independently exercised.

## Review record

PR #148: round 1 BLOCK (uploads spooled to disk over 1 MB, no CSRF protection, unbounded learning, URL guard bypasses, error reflection); round 2 APPROVE-WITH-CONDITIONS, conditions met in `292691e`.

PR #151: round 1 BLOCK (an int64 overflow built an unbalanced entry that reported itself balanced, reachable through the real parser; zero-valued lines; a parse slot leaked on a panic; test gaps), all fixed test-first; round 2 APPROVE on `02532da`, non-blocking findings taken in `ccf72b4`. Reviews are recorded independent agent passes, not four-eyes.

Merge: both PRs went to `main` as one merge by Mathias on 2026-11-25 06:45 (the agent merge gate refused because Build & Import, Deploy and Mirror are skipped by design on pull_request events, #153). Post-merge run 13522, read by job: Lint / Test / Vet, Build & Import, Deploy via GitOps, E2E, Mirror all success. The rubric review of the combined head returned BLOCKED on one line the author reads as a false positive (R-AGENT-02 at `apentry_test.go:397`, a credit-note test); advisory on this lane, unanswered by Mathias at the time of writing. The rubric review job (ADR-0010 lane) returned "no usable verdict" and so counts as **unreviewed by that lane**, not clean.
