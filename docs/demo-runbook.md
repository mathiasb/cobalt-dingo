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

`~/invoice-demo/bin/demo` was built from the head of PR #151 (`ccf72b4`, branch `feat/ap-entries`), which is **stacked on PR #148**; neither is merged, so this is **not `main`**. Both PRs were independently reviewed (#148 twice, #151 twice). After the merges, rebuild from `main`:

```
cd <a clean checkout of main>
go build -o ~/invoice-demo/bin/demo.new ./cmd/demo && mv ~/invoice-demo/bin/demo.new ~/invoice-demo/bin/demo
systemctl --user restart invoice-demo.service
```

A restart forgets every correction and every downloadable CSV (both are in memory only).

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

- Reads up to 4 invoice images per submit (and total size capped), shows the fields, proposes an expense account from past bookings with the vouchers behind it, and learns corrections in memory.
- For each trusted invoice it shows the **proposed accounts payable entry**: debit the expense account, debit input VAT (the account past bookings used, else 2641), credit 2440; a gap up to 0.50 balances on 3740. One CSV of all entries is downloadable for 30 minutes (20 submits at most, memory only). An invoice that does not reconcile, has no proposed account, or is a credit note gets **no entry** and the page says why.
- Entries are in the invoice currency, not converted to SEK, and reverse-charge VAT on foreign purchases is not modelled (the entry says so). See #152.
- **No accounting-system access of any kind.** Nothing is posted anywhere. Uploads are held in memory for one request and are not written to disk (pinned by a test with the temp directory watched).
- One model call at a time and at most 2 uploads being read at once (more get 503). One shared 150 s model deadline per submit. Corrections are capped at 200 until restart.
- The data is synthetic, and the page says so. The accuracy shown says nothing about a real company's books.

## Known limits, tracked in #149 and #152

The learning cap is global; `*.ts.net` model hosts are accepted for any tailnet; the default HTTP transport honours `HTTP_PROXY`; the page has not been viewed in a browser; checks were run from koala, so the path from outside the LAN has not been independently exercised.

## Review record

PR #148: round 1 BLOCK (uploads spooled to disk over 1 MB, no CSRF protection, unbounded learning, URL guard bypasses, error reflection); round 2 APPROVE-WITH-CONDITIONS, conditions met in `292691e`.

PR #151: round 1 BLOCK (an int64 overflow built an unbalanced entry that reported itself balanced, reachable through the real parser; zero-valued lines; a parse slot leaked on a panic; test gaps), all fixed test-first; round 2 APPROVE on `02532da`, non-blocking findings taken in `ccf72b4`. Reviews are recorded independent agent passes, not four-eyes. The rubric review job (ADR-0010 lane) returned "no usable verdict" and so counts as **unreviewed by that lane**, not clean.
