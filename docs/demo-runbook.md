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

`~/invoice-demo/bin/demo` is built from **`main`** at `2810d07` (the merge of PR #162, multi-format ingestion, which sits on top of #148/#151), deployed 2026-11-25 and checked live: every sample format, a multipart PDF upload, a PDF renamed to .png, a PDF+DOCX batch, the scan refusal, an unsupported file and a hostile XML. `run.sh` sets `DEMO_MARKITDOWN` to the converter virtualenv (see below). Rebuild after any later change:

```
cd <a clean checkout of main>
go build -o ~/invoice-demo/bin/demo.new ./cmd/demo && mv ~/invoice-demo/bin/demo.new ~/invoice-demo/bin/demo
systemctl --user restart invoice-demo.service
```

A restart forgets every correction and every downloadable CSV (both are in memory only).

## Converter dependency (PDF and Word)

The demo runs `markitdown` as a subprocess for PDF and Word. Install it once into its own virtualenv (not through `uvx`: that is a launcher which starts python itself and misbehaves under a memory limit):

```
uv venv ~/invoice-demo/markitdown-venv
uv pip install --python ~/invoice-demo/markitdown-venv/bin/python 'markitdown[docx,pdf]'
export DEMO_MARKITDOWN=$HOME/invoice-demo/markitdown-venv/bin/markitdown   # in run.sh
```

Do not install `markitdown[all]`: it pulls cloud document-intelligence converters. The child gets only `PATH`, `HOME`, the cache variables and `LANG`, never the login or the 1Password session, and runs with these bounds: 60 s wall clock, 45 s CPU, **2 GiB of address space**, 1 MiB of output. Those bounds exist because a 447 KB PDF whose content stream inflates took 3.1 GB, and a 90 KB DOCX 1.5 GB, in an unbounded converter (review of #162). 2 GiB is virtual memory: python and its libraries need most of 1.5 GiB of it, so do not lower it without running `go test ./internal/ingest` with the virtualenv present, which converts the real fixtures and a real inflating PDF. A DOCX XML part over 4 MiB, or a DOCX over 16 MiB when unpacked, is refused before conversion.

Other settings: `DEMO_TEXT_MODEL` (default `DEMO_MODEL`). If the converter is missing the demo starts with a warning and images and e-invoice XML still work; a PDF or Word upload then fails. A converter failure that a document causes (timeout, over the limits, crash) shows "This file could not be read" with status 422; a converter that cannot start is logged as `converter could not run` and shows the model-busy sentence.

Samples of every format: copy the generated documents next to the PNGs, named `NNN.pdf`, `NNN.docx`, `NNN.xml` (three digits), into `~/invoice-demo-data/invoices/`. Other names are ignored. Generate with `uv run scripts/gen-variety.py <dir>`; the files are fictional. Real third-party layouts (FATURA2, CC-BY-4.0, attribution in `docs/research/2026-11-25-real-layout-samples.md`) sit at `201`-`210.png`; their supplier history is synthetic.

## Exchange rates (SEK conversion)

The demo reads `~/invoice-demo-data/fx-rates.json` (override: `DEMO_FX`) at startup and makes **no network call for rates**. Refresh it, then restart the service:

```
python3 scripts/fetch-riksbank-rates.py ~/invoice-demo-data/fx-rates.json   # last 120 days; EUR USD DKK NOK GBP CHF
systemctl --user restart invoice-demo.service
```

The API rate-limits; the script waits and retries. The file is refused whole at startup if anything in it is implausible (a rate outside 0.0001 to 100000 SEK per unit, dates out of order, more than six decimals, an unknown field, over 1 MiB); the log then says `exchange-rate file refused` and the demo runs unconverted. A missing file is normal and logged as such. Rates are exact integers (millionths of a krona), never floats; conversion rounds half away from zero per line, and a difference of at most one minor unit per line goes to 3740.

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
- **Foreign currency:** with a rate file loaded (see below) a EUR, USD, DKK, NOK, GBP or CHF entry is converted to SEK line by line, exactly, with the foreign amounts, the rate, its date and its source kept beside it (page and CSV). Without a rate for the invoice date (none published within the 7 days before it) there is **no entry**, and the page says why. Without a rate file entries stay in the invoice currency and say so. The rate is Riksbanken's published daily rate and is indicative: it is not necessarily the rate your accountant uses. **Reverse-charge VAT on foreign purchases is not modelled** (the entry says so; #164). Exchange differences at payment and year-end revaluation are not modelled.
- **No accounting-system access of any kind.** Nothing is posted anywhere. Uploads are held in memory for one request and are not written to disk (pinned by a test with the temp directory watched).
- One model call at a time and at most 2 uploads being read at once (more get 503). One shared 150 s model deadline per submit. Corrections are capped at 200 until restart.
- The data is synthetic, and the page says so. The accuracy shown says nothing about a real company's books.

## Known limits, tracked in #149 and #152

The learning cap is global; `*.ts.net` model hosts are accepted for any tailnet; the default HTTP transport honours `HTTP_PROXY`; the page has not been viewed in a browser; checks were run from koala, so the path from outside the LAN has not been independently exercised.

## Review record

PR #148: round 1 BLOCK (uploads spooled to disk over 1 MB, no CSRF protection, unbounded learning, URL guard bypasses, error reflection); round 2 APPROVE-WITH-CONDITIONS, conditions met in `292691e`.

PR #151: round 1 BLOCK (an int64 overflow built an unbalanced entry that reported itself balanced, reachable through the real parser; zero-valued lines; a parse slot leaked on a panic; test gaps), all fixed test-first; round 2 APPROVE on `02532da`, non-blocking findings taken in `ccf72b4`. Reviews are recorded independent agent passes, not four-eyes.

Merge: both PRs went to `main` as one merge by Mathias on 2026-11-25 06:45 (the agent merge gate refused because Build & Import, Deploy and Mirror are skipped by design on pull_request events, #153). Post-merge run 13522, read by job: Lint / Test / Vet, Build & Import, Deploy via GitOps, E2E, Mirror all success. The rubric review of the combined head returned BLOCKED on one line the author reads as a false positive (R-AGENT-02 at `apentry_test.go:397`, a credit-note test); advisory on this lane, unanswered by Mathias at the time of writing. The rubric review job (ADR-0010 lane) returned "no usable verdict" and so counts as **unreviewed by that lane**, not clean.
