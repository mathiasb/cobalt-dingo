# Fortnox API – Kompakt referens

> Extraherat från `docs/fortnox-openapi.json` (OpenAPI 3.0.3).  
> Bas-URL: `https://api.fortnox.se/3/`  
> Autentisering: `Authorization: Bearer <access_token>` på varje anrop.

---

## Gemensamma konventioner

| Aspekt | Värde |
|--------|-------|
| Datum | `YYYY-MM-DD` |
| Belopp | `float64` (double) |
| Kontointervall | 1000–9999 |
| Teckenkodning | UTF-8, `Content-Type: application/json` |

---

## /3/vouchers

### GET – Lista verifikationer

**Query-parametrar**

| Parameter | Typ | Beskrivning |
|-----------|-----|-------------|
| `fromdate` | date | Från datum |
| `todate` | date | Till datum |
| `voucherseries` | string | Filtrera på verifikationsserie |
| `financialyear` | int32 | Filtrera på räkenskapsår-ID |
| `lastmodified` | date | Ändrade efter datum |
| `costcenter` | string | Kostnadssälle |

**Responsnyckel:** `{ "Vouchers": [ ...VoucherListItem ] }`

### POST – Skapa verifikation

**Request body:** `{ "Voucher": { ... } }`

| Fält | Typ | Krav | Notering |
|------|-----|------|----------|
| `Description` | string (1-200) | **REQUIRED** | Verifikationstext |
| `TransactionDate` | date | **REQUIRED** | `YYYY-MM-DD` |
| `VoucherSeries` | string | **REQUIRED** | T.ex. `"A"` |
| `Year` | int32 | **REQUIRED** | Räkenskapsår-ID |
| `VoucherRows` | array | **REQUIRED** | Minst 2 rader |
| `ReferenceType` | enum | — | `INVOICE`, `SUPPLIERINVOICE`, `MANUAL`, m.fl. |
| `ReferenceNumber` | string | — | Fakturanummer el. liknande |
| `Comments` | string (max 1000) | — | |
| `CostCenter` | string | — | |
| `Project` | string | — | |

**VoucherRow-fält**

| Fält | Typ | Krav | Notering |
|------|-----|------|----------|
| `Account` | int32 (1000-9999) | **REQUIRED** | BAS-kontonummer |
| `Debit` | float64 | — | Debit-belopp (exkl. moms) |
| `Credit` | float64 | — | Kredit-belopp (exkl. moms) |
| `Description` | string | — | Radtext |
| `TransactionInformation` | string (max 100) | — | |
| `CostCenter` | string | — | |
| `Project` | string | — | |

> **Balanskrav:** Summa Debit == Summa Credit (tolerans ±0,005 SEK).

---

## /3/vouchers/{VoucherSeries}/{VoucherNumber}

### GET – Hämta enskild verifikation

**Path-parametrar:** `VoucherSeries` (string), `VoucherNumber` (int32)  
**Query:** `financialyear` (int32)

**Response:** `{ "Voucher": { ...fält som POST ovan + VoucherNumber, VoucherSeries, Year } }`

---

## /3/invoices

### GET – Lista kundfakturor

**Query-parametrar** (urval)

| Parameter | Typ | Beskrivning |
|-----------|-----|-------------|
| `filter` | string | `cancelled`, `fullypaid`, `unpaid`, `unpaidoverdue` |
| `fromdate` / `todate` | date | Fakturadatumintervall |
| `customernumber` | string | Filtrera på kund |
| `financialyear` | int32 | Räkenskapsår-ID |

### POST – Skapa faktura

**Request body:** `{ "Invoice": { ... } }`

| Fält | Typ | Krav | Notering |
|------|-----|------|----------|
| `CustomerNumber` | string | **REQUIRED** | |
| `InvoiceRows` | array | **REQUIRED** | |
| `InvoiceDate` | date | — | |
| `DueDate` | date | — | |
| `InvoiceType` | enum | — | `INVOICE`, `AGREEMENTINVOICE`, `CASHINVOICE` |
| `Currency` | string (3) | — | `SEK` om ej annat |
| `OurReference` | string (max 50) | — | |
| `YourReference` | string (max 50) | — | |
| `YourOrderNumber` | string (max 75) | — | |
| `Remarks` | string (max 1024) | — | |
| `Language` | enum | — | `SV`, `EN` |

**InvoiceRow-fält**

| Fält | Typ | Notering |
|------|-----|----------|
| `ArticleNumber` | string | |
| `Description` | string (max 255) | |
| `Quantity` | float64 | |
| `Price` | float64 | Á-pris exkl. moms |
| `Account` | int32 | BAS-konto (t.ex. 3001) |
| `VAT` | float64 | Momssats i % (25, 12, 6, 0) |
| `Unit` | string (max 20) | |

**Nyckel-responsfält**

| Fält | Typ | Notering |
|------|-----|----------|
| `DocumentNumber` | string | Fakturanummer |
| `Total` | float64 | Inkl. moms |
| `TotalVAT` | float64 | Momsdel |
| `Net` | float64 | Exkl. moms |
| `Balance` | float64 | Utestående belopp |
| `Booked` | bool | Bokförd |
| `Sent` | bool | Skickad |
| `Cancelled` | bool | Makulerad |
| `VoucherNumber` | int32 | Kopplad verifikation |
| `VoucherSeries` | string | |

---

## /3/invoices/{DocumentNumber}

### GET – Hämta enskild faktura

**Path:** `DocumentNumber` (string)  
**Response:** `{ "Invoice": { ...alla fält ovan } }`

---

## /3/accounts

### GET – Lista konton (BAS-kontoplanen)

**Query:** `financialyear` (int32)

**Nyckel-responsfält per konto**

| Fält | Typ | Notering |
|------|-----|----------|
| `Number` | int32 | Kontonummer (1000-9999) |
| `Description` | string | Kontonamn |
| `Active` | bool | |
| `VATCode` | string | T.ex. `MP1`, `MP2`, `MP3`, `MF` |
| `SRU` | int32 | SRU-kod för INK2 |
| `BalanceBroughtForward` | float64 | Ingående balans |
| `BalanceCarriedForward` | float64 | Utgående balans |
| `Year` | int32 | Räkenskapsår-ID |

---

## /3/accounts/{Number}

### GET – Hämta enskilt konto

**Path:** `Number` (int32, 1000-9999)

---

## /3/assets

### GET – Lista anläggningstillgångar

**Nyckel-responsfält**

| Fält | Typ | Notering |
|------|-----|----------|
| `Id` | int32 | Intern ID |
| `Number` | string | Inventarienummer |
| `Description` | string | Benämning |
| `Type` | string | Tillgångstyp |
| `Status` | string | `Active`, `FullyDepreciated`, m.fl. |
| `AcquisitionDate` | date | Anskaffningsdatum |
| `AcquisitionValue` | int32 | Anskaffningsvärde (SEK) |
| `DepreciationMethod` | string | Avskrivningsmetod |
| `DepreciatedTo` | date | Avskrivet t.o.m. datum |
| `Group` | string | Grupp |
| `CostCenter` / `Project` | string | |
| `Brand` | string | Märke/tillverkare |

---

## /3/assets/{GivenNumber}

### GET – Hämta enskild tillgång

**Path:** `GivenNumber` (string) – inventarienummer eller ID

---

## /3/customers

### GET – Lista kunder

**Nyckel-fält per kund**

| Fält | Typ | Notering |
|------|-----|----------|
| `CustomerNumber` | string | Kundnummer |
| `Name` | string | Kundnamn |
| `Active` | bool | |
| `Email` | string | |
| `OrganisationNumber` | string | Org.nummer |
| `Currency` | string | |
| `TermsOfPayment` | string | |

---

## /3/suppliers

### GET – Lista leverantörer

**Nyckel-fält per leverantör**

| Fält | Typ | Notering |
|------|-----|----------|
| `SupplierNumber` | string | Leverantörsnummer |
| `Name` | string | Leverantörsnamn |
| `Active` | bool | |
| `Email` | string | |
| `OrganisationNumber` | string | |
| `PreDefinedAccount` | string (4) | Standard BAS-konto |
| `BG` / `PG` | string | Bankgiro / Plusgiro |
| `IBAN` / `BIC` | string | Internationell betalning |

---

## /3/supplierinvoices

### GET – Lista leverantörsfakturor

**Query-parametrar** (urval)

| Parameter | Typ | Beskrivning |
|-----------|-----|-------------|
| `filter` | string | `cancelled`, `fullypaid`, `unpaid`, `unpaidoverdue` |
| `fromdate` / `todate` | date | Fakturadatumintervall |
| `suppliernumber` | string | Filtrera på leverantör |
| `financialyear` | int32 | |

**Nyckel-responsfält**

| Fält | Typ | Notering |
|------|-----|----------|
| `GivenNumber` | string | Internt fakturanummer |
| `SupplierNumber` | string | |
| `SupplierName` | string | |
| `InvoiceNumber` | string | Leverantörens fakturanr |
| `InvoiceDate` / `DueDate` | date | |
| `Total` | string | Totalt belopp |
| `VAT` | float64 | Moms |
| `Balance` | string | Utestående |
| `Booked` | bool | |
| `Cancelled` | bool | |
| `OCR` | string | OCR-nummer |
| `VoucherNumber` / `VoucherSeries` | — | Kopplad verifikation |
| `SupplierInvoiceRows` | array | Rader |

**SupplierInvoiceRow-fält**

| Fält | Typ | Notering |
|------|-----|----------|
| `Account` | int32 | BAS-konto |
| `Code` | enum | `TOT`, `VAT`, `FRT`, `AFE`, m.fl. |
| `Description` | string | |
| `Quantity` / `Price` | float64 | |
| `VAT` | float64 | Momssats |
| `VATCode` | string | |
| `CostCenter` / `Project` | string | |

---

## /3/supplierinvoices/{GivenNumber}

### GET – Hämta enskild leverantörsfaktura

**Path:** `GivenNumber` (string)

---

## /3/financialyears

### GET – Lista räkenskapsår

**Query:** `date` (date) – filtrera på specifikt datum

**Responsfält**

| Fält | Typ | Notering |
|------|-----|----------|
| `Id` | int32 | Räkenskapsår-ID (används i andra anrop) |
| `FromDate` | date | Startdatum |
| `ToDate` | date | Slutdatum |
| `AccountingMethod` | enum | `ACCRUAL`, `CASH` |
| `AccountChartType` | string | Kontoplanstyp |

---

## /3/companyinformation

### GET – Hämta företagsinformation

**Responsfält**

| Fält | Typ |
|------|-----|
| `CompanyName` | string |
| `OrganizationNumber` | string |
| `Address`, `ZipCode`, `City`, `Country` | string |
| `DatabaseNumber` | int32 |

---

## /3/settings/lockedperiod

### GET – Hämta låst period

**Responsexempel**
```json
{ "LockedPeriod": { "EndDate": "2024-12-31" } }
```
`EndDate` är `null` om ingen period är låst.

---

## /3/me

### GET – Hämta inloggad användare

**Responsfält**

| Fält | Typ |
|------|-----|
| `Id` | string |
| `Name` | string |
| `Email` | string |
| `Locale` | string |
| `SysAdmin` | bool |

---

## Felformat

```json
{
  "ErrorInformation": {
    "error": 2000417,
    "message": "Du har inte rättigheter till denna resurs.",
    "code": 401
  }
}
```

Vid 429 (rate limit): exponentiell backoff, max 3 försök.  
Vid 503: retry efter 5 s, max 2 försök.
