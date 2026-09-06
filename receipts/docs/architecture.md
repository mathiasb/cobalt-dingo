# Arkitektur – coo-agent

## Översikt

coo-agent är ett agentiskt system som ger Claude säker, granskningsbar åtkomst till en enskild
firmas bokföring i Fortnox. Systemet är byggt kring principen att **läsa fritt, skriva med
bekräftelse, aldrig radera**.

```
┌─────────────────────────────────────────────────────────────────┐
│                        Användaren (chatt)                        │
└────────────────────────────┬────────────────────────────────────┘
                             │
                    ┌────────▼────────┐
                    │  Claude (LLM)   │
                    │  + MCP-klient   │
                    └────────┬────────┘
                             │  MCP-protokoll (stdio)
          ┌──────────────────▼──────────────────────┐
          │              MCP-server                  │
          │  (exponerar verktyg, hanterar sessions)  │
          └──────┬──────────────┬────────────────────┘
                 │              │
        ┌────────▼───┐   ┌──────▼──────┐
        │ API-klient │   │  Validator  │
        │  Fortnox   │   │  BAS/moms   │
        └────────┬───┘   └─────────────┘
                 │
        ┌────────▼───────────┐
        │  Fortnox REST API  │
        │  api.fortnox.se/3/ │
        └────────────────────┘

Sidokomponenter (körs parallellt):
  ┌────────────┐   ┌────────────────┐   ┌──────────────┐
  │ Audit-logg │   │   Scheduler    │   │ Token-store  │
  │ append-only│   │ cron-baserad   │   │ krypterad    │
  └────────────┘   └────────────────┘   └──────────────┘
```

---

## Teknologival

| Komponent       | Val                    | Motivering                                      |
|-----------------|------------------------|-------------------------------------------------|
| Språk           | Go 1.24+               | Single binary, snabb, bra cross-compilation     |
| MCP-ramverk     | `mark3labs/mcp-go`     | De facto standard Go MCP-implementation         |
| HTTP-klient     | `net/http` + `resty`   | Inbyggt + enkel retry/middleware                |
| OAuth2          | `golang.org/x/oauth2`  | Officiellt Google-paket, fungerar med Fortnox   |
| Kryptering      | `golang.org/x/crypto`  | Välbeprövat                                     |
| Nyckelring      | `zalando/go-keyring`   | Plattformsoberoende, systemnyckelring            |
| Scheduler       | `robfig/cron/v3`       | Cron-syntax, produktionsbeprövad                |
| E-post          | IMAP via `emersion/go-imap` | Protokollstandard – ej inlåst till Gmail   |
| PDF-parsning    | `pdfcpu` eller `unipdf`| Extrahera belopp/datum ur faktura-PDF:er        |
| Testramverk     | `testing` + `testify`  | Standard Go                                     |
| Webb-UI (senare)| `html/template` + HTMX | Om admin-gränssnitt behövs framöver             |

### Kompilerade targets

| Host    | OS/Arch          | Notering                        |
|---------|------------------|---------------------------------|
| koala   | `linux/amd64`    | Primär host för scheduler       |
| piguard | `linux/arm64`    | Raspberry Pi 4/5                |
| piblock | `linux/arm64`    | Raspberry Pi 4/5                |
| iguana  | `darwin/arm64`   | Mac Studio M2 Ultra             |

Rekommendation: kör schedulern på **koala** (Linux-server, alltid igång).

---

## Komponenter

### 1. MCP-server (`internal/mcp/`)

Exponerar verktyg mot Claude via Model Context Protocol över stdio.
Ansvarar för:

- Registrering av alla tillgängliga verktyg (läs, skriv, rapport)
- Säkerhetscheck: blockerar röd-nivå-operationer innan de når API-klienten
- Vidarebefordran av anrop till rätt underkomponent

**Verktygsgrupper:**

| Grupp              | Verktyg                                  | Nivå |
|--------------------|------------------------------------------|------|
| Fakturor           | `list_invoices`, `get_invoice`           | Grön |
| Fakturor           | `create_invoice`, `update_invoice`       | Gul  |
| Verifikationer     | `list_vouchers`, `get_voucher`           | Grön |
| Verifikationer     | `create_voucher`                         | Gul  |
| Kunder/Leverantörer| `list_customers`, `list_suppliers`       | Grön |
| Konton             | `list_accounts`                          | Grön |
| Anläggningar       | `list_assets`                            | Grön |
| Momsrapport        | `get_vat_report`                         | Grön |
| Likviditet         | `get_balance_overview`                   | Grön |
| Inkorgsbevakning   | `check_inbox`                            | Grön |
| PDF-inläsning      | `process_invoice_pdf`                    | Grön |

### 2. API-klient (`internal/api/`)

Hanterar all kommunikation med Fortnox REST API.

- **OAuth 2.0** – Authorization Code Flow, automatisk token-refresh
- **Retry-logik** – exponentiell backoff vid 429 (3 försök) och 503 (2 försök)
- **Rate limiting** – max 250 anrop/sekund, intern kö vid överskridning
- **Sandboxläge** – aktiveras via `FORTNOX_ENV=sandbox` i `.env`
- Loggar varje anrop till audit-loggen

**Kontoplan:** hämtas live från Fortnox `/accounts` vid start och cachas i minnet.
Ingen manuell BAS-JSON att underhålla.

```
internal/api/
  client.go      # Bas-HTTP-klient med retry och rate limiting
  auth.go        # OAuth2-flöde, token-refresh
  invoices.go    # Faktura-endpoints
  vouchers.go    # Verifikations-endpoints
  customers.go   # Kund-endpoints
  suppliers.go   # Leverantörs-endpoints
  accounts.go    # Konto-endpoints (inkl. kontoplanskache)
  assets.go      # Anläggnings-endpoints
  vat.go         # Momsrapport-endpoints
```

### 3. Validator (`internal/validator/`)

Validerar verifikationer och bokföringsdata.

Valideringsregler:
- Alla konton existerar i live-kontoplanen (från Fortnox)
- Verifikation är balanserad: `sum(debet) == sum(kredit)`, tolerans 0 SEK
- Momskod matchar kontotyp (t.ex. konto 6212 → MP1)
- Datum inom rimlig period (max 1 år bakåt, ej framåt)
- Verifikationstext är ifylld och beskrivande

### 4. Audit-logg (`internal/audit/`)

Append-only loggning av alla API-anrop och agentbeslut.

- **Format:** JSON Lines (ett JSON-objekt per rad)
- **Sökväg:** `~/.coo-agent/audit.log`
- **Rensas aldrig automatiskt**

```json
{
  "ts": "2025-06-15T08:32:11.423Z",
  "op": "GET",
  "endpoint": "/invoices",
  "params": {"filter": "unpaid"},
  "status": 200,
  "duration_ms": 142,
  "agent": "read-only-reporter"
}
```

### 5. Scheduler (`internal/scheduler/`)

Cron-baserad bevakning. Kör **uteslutande läs-only + notifieringar** – inga
skrivoperationer utan interaktiv bekräftelse.

| Jobb                  | Schema        | Beskrivning                                      |
|-----------------------|---------------|--------------------------------------------------|
| `daily_briefing`      | `0 8 * * *`   | Daglig genomgång, notifierar om åtgärdspunkter   |
| `inbox_check`         | `0 8 * * *`   | IMAP-sökning efter fakturor/kvitton              |
| `vat_deadline_check`  | `0 9 1 * *`   | Påminnelse om kommande momsdeadline              |
| `expense_reminder`    | `0 9 1 * *`   | Påminnelse om återkommande utlägg att bokföra    |
| `invoice_reminder`    | Per schema     | Notifiering: "Dags att hämta WorkforceLogiq-faktura" |

> **Obs:** Mynt-avstämning är parkerad tills API-access eller bättre lösning finns.

### 6. Token-store (`internal/auth/`)

Säker hantering av OAuth-tokens för Fortnox (och framtida IMAP/Google).

- Tokens krypteras med AES-GCM
- Krypteringsnyckel hämtas från systemnyckelring via `go-keyring`
- Lagras på disk: `~/.coo-agent/tokens.enc`
- Tokens loggas aldrig
- Fortnox-refresh sker automatiskt 60 sekunder före utgång

### 7. E-postbevakning (`internal/mail/`)

IMAP-baserad, fungerar med valfri e-postleverantör (Google Workspace, Fastmail,
Proton Mail, etc.). Inga leverantörsspecifika API:er.

- Söker inkorgen efter avsändare: Telenor, Telia, Bahnhof, BMW Financial Services,
  Söderberg & Partners, Fortnox, Skatteverket
- Laddar ner bilagor (PDF-fakturor) till bevakad mapp för vidare bearbetning
- Raderar aldrig e-post
- Öppnar aldrig bilagor automatiskt utan att notera dem för granskning

---

## Integrationer

### Fortnox
- REST API: `https://api.fortnox.se/3/`
- OAuth 2.0 Authorization Code Flow
- Sandbox: samma URL, separata testcredentials
- Redirect URI för initial setup: `http://localhost:8080/callback`

### WorkforceLogiq (kundportal)
- URL: `https://eu.workforcelogiq.com`
- Kräver användarnamn/lösenord + SMS OTP var 30:e dag
- **Kan inte automatiseras** pga SMS-steget
- Flöde: systemet notifierar när faktura borde finnas → användaren loggar in och
  laddar ner PDF → PDF läggs i bevakad mapp → systemet läser och föreslår bokning

### Kivra (BMW Financial Services)
- Kivra saknar publikt API
- **Lösning:** aktivera e-postvidarebefordran i Kivra → BMW-fakturor landar i
  företagsinkorgen → IMAP-bevakningen fångar dem automatiskt

### E-post (IMAP)
- Protokoll: IMAP4 med TLS
- Konfigureras med host/port/credentials i `.env`
- Fungerar med Google Workspace idag, migrerbart till EU-alternativ utan kodändring

### Mynt (företagskort)
- Inget publikt API tillgängligt
- **Parkerad:** hanteras manuellt tills vidare
- Framtida alternativ: CSV-export från Mynt-portalen + importverktyg

---

## Specifika use cases

### Återkommande privata utlägg (Telenor, Telia, Bahnhof)
Fakturor anländer via e-post → IMAP-bevakning identifierar dem →
`book-expense`-kommandot föreslår verifikation → användaren bekräftar.

Schema (CLAUDE.md):

| Leverantör | Periodicitet | Konto | Momskod |
|------------|-------------|-------|---------|
| Telenor    | Månad        | 6212  | MP1     |
| Telia      | Månad        | 6212  | MP1     |
| Bahnhof    | Kvartal      | 6212  | MP1     |

### Finansiellt leasing – BMW Financial Services
Finansiellt leasing innebär att bilen finns i anläggningsregistret.
Varje månad behövs tre separata bokningar:

| Händelse          | Debet              | Kredit             |
|-------------------|--------------------|--------------------|
| Leasingbetalning  | 2350 (amortering)  | 1930 Bankkonto     |
|                   | 6310 (ränta)       |                    |
| Avskrivning       | 7832               | 1229               |

Kräver: amorteringsplan från BMW Financial Services (totalskuld, räntesats,
amortering per månad). Systemet läser planen och föreslår korrekt uppdelning
varje månad.

> **OBS:** Privat användning av leasingbil i enskild firma begränsar
> momsavdragsrätten. Normalt max 50% ingående moms på bilrelaterade kostnader.
> Dokumentera verklig tjänsteandel.

### WorkforceLogiq-faktura (enda kunden)
1. Scheduler notifierar vid förväntad fakturadatum
2. Användaren loggar in manuellt och laddar ner PDF
3. PDF placeras i `~/.coo-agent/inbox/`
4. Systemet läser PDF, extraherar belopp och period
5. Bokning föreslås (konto 3001, utgående moms 25%)
6. Användaren bekräftar → verifikation skapas i Fortnox

### Momsrapport (kvartalsvis)
Läs-only sammanställning från Fortnox. Presenteras för granskning.
**Skickas aldrig automatiskt till Skatteverket.**

Deadlines:
- Q1 → 12 maj
- Q2 → 12 aug
- Q3 → 12 nov
- Q4 → 12 feb

---

## Säkerhetsmodell

Se `.claude/rules/security.md` för fullständig beskrivning.

```
Grön  →  Kör automatiskt    (alla GET-operationer, PDF-läsning, IMAP-läsning)
Gul   →  Kräver "ja"        (POST/PUT till Fortnox)
Röd   →  Alltid blockerat   (DELETE, momsdeklaration till Skatteverket, tredjepartsdelning)
```

MCP-servern tillämpar nivåerna programmatiskt – röd-nivå-operationer blockeras
innan de når API-klienten.

---

## Katalogstruktur

```
coo-agent/
├── cmd/
│   └── coo-agent/
│       └── main.go              # Entrypoint – startar MCP-server + scheduler
├── internal/
│   ├── mcp/                     # MCP-server, verktygsregistrering
│   ├── api/                     # Fortnox API-klient
│   ├── validator/               # BAS/moms-validering
│   ├── audit/                   # Audit-logg
│   ├── scheduler/               # Cron-schemaläggning
│   ├── auth/                    # Token-hantering, OAuth2-flöde
│   ├── mail/                    # IMAP-bevakning
│   └── pdf/                     # PDF-parsning för faktura-inläsning
├── docs/
│   └── architecture.md          # Det här dokumentet
├── testdata/                    # Testfixtures (exempel-PDF:er, mock-svar)
├── go.mod
├── go.sum
├── CLAUDE.md
├── .env.example
└── .gitignore

# Runtime-data (utanför repo)
~/.coo-agent/
├── tokens.enc                   # Krypterade OAuth-tokens
├── audit.log                    # Audit-logg (append-only)
└── inbox/                       # Bevakad mapp för inkommande PDF:er
```

---

## Dataflöde – exempel: bokföra Telenor-faktura

```
IMAP-scheduler hittar e-post från Telenor
    │
    ▼
Bilaga (PDF) sparas i ~/.coo-agent/inbox/
    │
    ▼
pdf-parser extraherar: belopp 450 kr, period mars 2025
    │
    ▼
Claude notifieras / användaren frågar "vad har kommit in?"
    │
    ▼
Ekonomirapportör kontrollerar: finns redan verifikation för Telenor mars?
    │
    ▼
Validator bygger förslag:
  Debet  6212  360,00 SEK  (kostnad exkl. moms)
  Debet  2640   90,00 SEK  (ingående moms 25%)
  Kredit 2893  450,00 SEK  (skuld till ägare)
  Kontroll: 450 = 450 ✓
    │
    ▼
Claude presenterar förslaget
    │
    ▼
Användaren: "ja"
    │
    ▼
Bokförare anropar POST /vouchers
    │
    ▼
Audit-loggen uppdateras, verifikationsnummer bekräftas
```
