# Fortnox API Discovery Report

**Date**: 2026-04-15
**Scope**: Map the AP/AR automation MVP workflow to available Fortnox API endpoints.

---

## Summary

| Step | Status | Key finding |
|------|--------|-------------|
| Scan inbox documents | Partial | Raw files (PDFs) only; no structured invoice data |
| Scan e-invoice channel | Partial | Peppol invoices surface as SupplierInvoices; no channel-of-origin field |
| Scan manually imported invoices | Confirmed | `/3/supplierinvoices` + WebSocket events |
| Extract supplier details (IBAN/BIC) | Confirmed | On Supplier entity, not invoice — extra API call required |
| Extract invoice fields | Confirmed | Currency, CurrencyRate, DueDate, OCR, ExternalInvoiceNumber all present |
| Filter for foreign currency | Partial | Currency field exists; no server-side filter — must filter client-side |
| Assemble PAIN.001 batch | Gap | No payment-initiation API in Fortnox; PAIN.001 is our responsibility |
| User review UI + single PSD2 approval | Gap | Entirely our product layer; single integration with PISP platform, covers top ~20 Nordic/N.European banks |
| Submit to bank via PSD2 | Gap | Not in Fortnox scope |
| Record bank confirmation + actual FX/fees | Confirmed | Payment write-back via API confirmed; actual FX/fees sourced from AISP bank data (cobalt-dingo holds AISP license) |
| Book FX gain/loss to BAS account | Confirmed | `POST /3/vouchers` with account rows |
| Book bank fees to BAS account | Confirmed | Same voucher API |
| Mark invoice as paid | Confirmed | `POST /3/supplierinvoicepayments` + bookkeep action |

---

## Invoice detection

### All three sources converge on one API surface

Inbox documents, Peppol e-invoices, and manually imported invoices all end up as records in `/3/supplierinvoices`. Treating them as three separate API surfaces is unnecessary complexity.

**Recommended approach:**
1. Subscribe to `wss://ws.fortnox.se/topics-v1` (Kafka-backed, per-tenant) for real-time events
2. Available events: `supplier-invoice-created-v1`, `supplier-invoice-updated-v1`, `supplier-invoice-cancelled-v1`, `supplier-invoice-bookkeep-v1`
3. On startup: poll `GET /3/supplierinvoices` with `lastmodified` filter to catch up on missed events (up to 14 days of WebSocket replay available)
4. WebSocket connection supports multiple tenants in one connection — important for multi-tenant architecture

### Inbox (`/3/inbox`)
- `GET /3/inbox` — folder tree with raw files (PDFs, TIFs, JPGs)
- `GET /3/inbox/{id}` — single file
- `POST` — upload file, `DELETE` — remove
- No structured invoice fields; no currency metadata
- Files link to supplier invoices via SupplierInvoiceFileConnections after OCR processing
- **Verdict**: low value for our scanning workflow; skip unless we need to inspect unprocessed PDFs

### E-invoice channel (Peppol/PINT)
- `GET /3/invoices/{id}/einvoice` is for **outgoing** customer invoices only
- Incoming Peppol invoices are auto-processed by Fortnox's access point and appear as SupplierInvoice records
- No API field distinguishes Peppol-sourced vs. manually imported invoices
- **Verdict**: transparent via the SupplierInvoice resource; no special handling needed

---

## Invoice fields

### SupplierInvoice resource

| Field | Type | Notes |
|-------|------|-------|
| `GivenNumber` | string | Fortnox internal ID |
| `SupplierNumber` | string | Links to Supplier entity |
| `SupplierName` | string | Denormalized copy |
| `InvoiceDate` | date | |
| `DueDate` | date | |
| `Currency` | string | 3-letter ISO code (e.g. "EUR", "USD") |
| `CurrencyRate` | float | Rate vs SEK; auto-filled if omitted on create |
| `CurrencyUnit` | int | Currency units per rate (usually 1) |
| `Total` | float | Total in SEK |
| `TotalInvoiceCurrency` | float | Total in invoice currency |
| `TotalVAT` | float | VAT in SEK |
| `Balance` | float | Outstanding balance |
| `OCR` | string | OCR/giro payment reference |
| `ExternalInvoiceNumber` | string | Supplier's own invoice number — use as PAIN.001 payment reference |
| `ExternalInvoiceSeries` | string | Supplier's invoice series |
| `Booked` | bool | Whether bookkeeping entry has been created |
| `Cancelled` | bool | |
| `PaymentPending` | bool | |

### IBAN/BIC location — critical

IBAN and BIC are on the **Supplier entity**, not on SupplierInvoice.

```
GET /3/suppliers/{SupplierNumber}
```

Relevant Supplier fields:
- `IBAN` — for international and in-Sweden foreign-currency payments
- `BIC` — required with IBAN
- `BankAccountNumber` — Swedish domestic account
- `BankgiroNumber` — BG (domestic SEK)
- `PlusgiroNumber` — PG (domestic SEK)

Fortnox routing rule: for international payments **or** in-Sweden payments in foreign currency → always IBAN+BIC, never BG/PG/account.

**Implication**: every invoice in a batch requires a separate `GET /3/suppliers/{id}` call. Cache with short TTL. Rate limit is 25 req/5 sec.

---

## Payment recording write-back

### SupplierInvoicePayment resource

Endpoint: `POST /3/supplierinvoicepayments`

| Field | Notes |
|-------|-------|
| `InvoiceNumber` | Links to SupplierInvoice.GivenNumber |
| `Amount` | Actual payment amount in SEK |
| `AmountCurrency` | Amount in original invoice currency |
| `Currency` | 3-letter code |
| `CurrencyRate` | **Actual execution rate from bank** — use this, not the rate from the invoice |
| `CurrencyUnit` | |
| `PaymentDate` | Actual settlement date from bank confirmation |
| `ModeOfPayment` | Must reference a valid code from `/3/modesofpayments` |
| `Source` | Free text — set to e.g. "cobalt-dingo" |
| `Booked` | Read-only; set by bookkeep action |

Bookkeep action: `PUT /3/supplierinvoicepayments/{Number}/bookkeep`
- This auto-creates an accounting voucher for the payment
- **Does not automatically book the FX delta** (difference between invoice rate and execution rate) — that is our responsibility (see FX gain/loss section)

---

## PAIN.001 assembly — critical architectural finding

**Fortnox generates PAIN.001 files internally** for its bank connection module (supporting SEB, Handelsbanken, Swedbank, Nordea, Danske Bank). This module is accessible **only via the Fortnox UI**, not through any API.

The SupplierInvoicePayments API is a write-back/accounting endpoint — it records that a payment was made. There is no API endpoint to trigger a Fortnox payment file export or submit payments to a bank.

**Consequence**: our product must:
1. Extract validated invoice rows from Fortnox API
2. Extract IBAN/BIC from Supplier entity
3. Build ISO 20022 PAIN.001 XML ourselves
4. Submit via PISP bank partner's platform API (single integration, reaches top ~20 Nordic/Northern European banks)
5. Write back actual execution data to Fortnox after bank confirmation

---

## GL write-back: vouchers

Endpoint: `POST /3/vouchers`

Required scope: `bookkeeping`

Pre-validation required before each voucher POST:
1. Financial year exists for the booking date: `GET /3/financialyears?date={date}`
2. Accounts exist and are active: `GET /3/accounts/{number}`
3. Voucher series exists for the financial year

VoucherRow fields: `Account`, `Debit`, `Credit`, `TransactionInformation`, `CostCenter` (optional)

Relevant BAS 2024 accounts:
| Account | Description |
|---------|-------------|
| `3960` | FX gains on AR |
| `7960` | FX losses on AP |
| `6570` | Bank charges / payment fees |
| `1930` | Bank account (debit on payment) |
| `2440` | Accounts payable (credit cleared on payment) |

**FX gain/loss calculation**: when `supplierinvoicepayments` is bookkeept, Fortnox records the payment at the actual execution rate. The difference vs. the original invoice booking rate must be calculated by us and posted as a separate voucher to 3960/7960.

---

## Failure modes

| Failure | Handling |
|---------|----------|
| Missing/invalid IBAN | Validate IBAN (checksum + country format) before PAIN.001 assembly; surface as validation error if empty or malformed on Supplier entity |
| Payment rejected by bank | PSD2 layer returns rejection; do not write SupplierInvoicePayment; surface rejection in UI |
| FX rate outside tolerance | Check actual execution rate from PSD2 response vs. indicative rate from `/3/currencies`; tolerance threshold is our business rule |
| Fortnox period locked | Voucher POST returns API error; catch and surface to user with instructions to unlock period in Fortnox |
| Supplier has no IBAN (BG/PG only) | Detected at validation time (IBAN empty, BIC empty on Supplier); foreign-currency invoice without IBAN = hard block |

---

## OAuth2 scopes

| Scope | Resource |
|-------|----------|
| `supplierinvoice` | SupplierInvoices, SupplierInvoicePayments, accruals, file connections |
| `supplier` | Supplier entity (IBAN/BIC lookup) |
| `bookkeeping` | Vouchers, accounts, financial years, voucher series |
| `payment` | Invoice and supplier invoice payment resources |
| `currency` | Exchange rates |
| `inbox` | Inbox files (low priority) |
| `companyinformation` | Per-tenant company data |

All scopes grant both read and write — no read-only mode available.

---

## What needs sandbox confirmation

- Exact error code when posting a voucher to a locked accounting period
- Whether `PaymentPending` can be set via API or is Fortnox-internal only
- Whether `PUT .../supplierinvoicepayments/{id}/bookkeep` with a CurrencyRate differing from the invoice rate auto-creates an FX delta voucher, or whether that is entirely our responsibility
- Whether incoming Peppol invoices have any distinguishing field vs. manually created ones
- WebSocket per-tenant token isolation behaviour in multi-tenant setup

---

## Rate limits

- 25 requests per 5 seconds (300/minute) per tenant
- A batch of 50 invoices = ~100 API calls (invoice + supplier lookup each) = ~20 seconds minimum at full rate
- Concurrency + per-tenant supplier cache required from day one
