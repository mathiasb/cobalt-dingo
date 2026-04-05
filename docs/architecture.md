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
                             │  MCP-protokoll
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

## Komponenter

### 1. MCP-server (`src/mcp/`)

Exponerar verktyg mot Claude via Model Context Protocol. Ansvarar för:

- Registrering av alla tillgängliga verktyg (läs, skriv, rapport)
- Sessionhantering och kontextbegränsning
- Vidarebefordran av anrop till rätt underkomponent
- Säkerhetscheck: blockerar röd-nivå-operationer innan de når API-klienten

**Verktygsgrupper som exponeras:**

| Grupp | Verktyg | Nivå |
|-------|---------|------|
| Fakturor | `list_invoices`, `get_invoice` | Grön |
| Fakturor | `create_invoice`, `update_invoice` | Gul |
| Verifikationer | `list_vouchers`, `get_voucher` | Grön |
| Verifikationer | `create_voucher` | Gul |
| Kunder/Leverantörer | `list_customers`, `list_suppliers` | Grön |
| Konton | `list_accounts` | Grön |
| Anläggningar | `list_assets` | Grön |
| Momsrapport | `get_vat_report` | Grön |
| Likviditet | `get_balance_overview` | Grön |

### 2. API-klient (`src/api/`)

Hanterar all kommunikation med Fortnox REST API.

- **OAuth 2.0** – Authorization Code Flow, automatisk token-refresh
- **Retry-logik** – exponentiell backoff vid 429/503 (se `rules/fortnox-api.md`)
- **Rate limiting** – max 250 anrop/sekund, intern kö vid överskridning
- **Sandboxläge** – aktiveras via `FORTNOX_ENV=sandbox` i `.env`
- Loggar varje anrop till audit-loggen (timestamp, endpoint, metod, statuskod, ms)

```
src/api/
  client.py        # Bas-HTTP-klient med retry och rate limiting
  auth.py          # OAuth2-flöde, token-refresh, token-lagring
  invoices.py      # Faktura-endpoints
  vouchers.py      # Verifikations-endpoints
  customers.py     # Kund-endpoints
  suppliers.py     # Leverantörs-endpoints
  accounts.py      # Konto-endpoints
  assets.py        # Anläggnings-endpoints
  vat.py           # Momsrapport-endpoints
```

### 3. Validator (`src/validator/`)

Validerar verifikationer och bokföringsdata innan de presenteras för användaren eller
skickas till Fortnox.

Valideringsregler:
- Alla konton existerar i BAS 2024-kontoplanen
- Verifikation är balanserad: `sum(debet) == sum(kredit)`, tolerans 0 SEK
- Momskod matchar kontotyp (t.ex. konto 6212 → MP1)
- Datum inom rimlig period (max 1 år bakåt, ej framåt)
- Verifikationstext är ifylld och beskrivande

```
src/validator/
  balance.py       # Balanscheck debet/kredit
  accounts.py      # BAS-kontoplanskontroll
  vat_rules.py     # Momskod–konto-validering
  dates.py         # Datumgränskontroll
  bas_2024.json    # BAS-kontoplan som JSON-uppslagsverk
```

### 4. Audit-logg (`src/audit/`)

Append-only loggning av alla API-anrop och agentbeslut.

- **Format:** JSON Lines (ett JSON-objekt per rad)
- **Sökväg:** `~/.coo-agent/audit.log`
- **Rensas aldrig automatiskt**
- Känsliga parametrar (belopp, personnummer) maskeras i läsbart gränssnitt men loggas i full

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

### 5. Scheduler (`src/scheduler/`)

Cron-baserad bevakning av deadlines och återkommande utlägg.

Schemalagda jobb:

| Jobb | Schema | Beskrivning |
|------|--------|-------------|
| `daily_briefing` | `0 8 * * *` | Daglig genomgång, notifierar vid åtgärdspunkter |
| `monitor_inbox` | `0 8 * * *` | Söker e-post efter fakturor/kvitton |
| `mynt_reconcile` | `0 9 * * 1` | Veckovis Mynt-avstämning (måndag) |
| `vat_deadline_check` | `0 9 1 * *` | Påminnelse om kommande momsdeadline |
| `expense_reminder` | `0 9 1 * *` | Påminnelse om återkommande utlägg |

### 6. Token-store (`src/auth/`)

Säker hantering av OAuth-tokens.

- Tokens krypteras med Fernet (symmetrisk kryptering)
- Krypteringsnyckel hämtas från systemnyckelring via `keyring`-biblioteket
- Lagras på disk: `~/.coo-agent/tokens.enc`
- Tokens loggas aldrig – inte ens maskerade versioner
- Refresh sker automatiskt 60 sekunder före utgång

---

## Agenter

Systemet definierar två specialiserade sub-agenter som Claude kan delegera till:

### Ekonomirapportör (`read-only-reporter`)
Läs-only. Hämtar, analyserar och rapporterar ekonomisk data. Kan aldrig skriva till Fortnox.
Se `.claude/agents/read-only-reporter.md`.

### Bokförare (`bookkeeper`)
Skrivbehörighet – men kräver alltid explicit "ja" från användaren innan varje operation.
Validerar fullständigt innan förslag presenteras. Se `.claude/agents/bookkeeper.md`.

---

## Integrationer

### Fortnox
- REST API, bas-URL: `https://api.fortnox.se/3/`
- OAuth 2.0, Authorization Code Flow
- Sandbox: samma URL, separata testcredentials

### Mynt (företagskort)
- Integration via Mynt API (credentials konfigureras i `.env` när tillgängligt)
- Fallback: CSV-export från Mynt-portalen parsas lokalt
- Används för kvittomatchning i `mynt-reconcile`-skill

### Gmail
- Läsåtkomst via Google OAuth2 / Gmail API
- Söker efter fakturor och kvitton från kända leverantörer
- Öppnar aldrig bilagor automatiskt
- Raderar aldrig e-post
- Används av `monitor-inbox`-skill

---

## Säkerhetsmodell

Se `.claude/rules/security.md` för fullständig beskrivning. Kortfattat:

```
Grön  →  Kör automatiskt    (alla GET-operationer)
Gul   →  Kräver "ja"        (POST/PUT till Fortnox)
Röd   →  Alltid blockerat   (DELETE, momsdeklaration, tredjepartsdelning)
```

MCP-servern tillämpar dessa nivåer programmatiskt – röd-nivå-operationer
blockeras i koden och når aldrig API-klienten.

---

## Katalogstruktur

```
coo-agent/
├── CLAUDE.md                    # Projektkonfiguration för Claude
├── .env.example                 # Miljövariabelsmall (committas)
├── .env                         # Verkliga värden (committas ALDRIG)
├── .gitignore
├── docs/
│   └── architecture.md          # Det här dokumentet
├── src/
│   ├── mcp/                     # MCP-server och verktygsregistrering
│   ├── api/                     # Fortnox API-klient
│   ├── validator/               # BAS/moms-validering
│   ├── audit/                   # Audit-logg
│   ├── scheduler/               # Cron-schemaläggning
│   └── auth/                    # Token-hantering
├── tests/
│   ├── unit/                    # Enhetstester per komponent
│   └── integration/             # Integrationstester mot Fortnox sandbox
├── .claude/
│   ├── agents/                  # Sub-agentdefinitioner
│   ├── commands/                # Slash-kommandon
│   ├── rules/                   # Regler och konventioner
│   ├── skills/                  # Auto-aktiverade skills
│   └── settings.json            # Behörighetsinställningar
└── ~/.coo-agent/                # Runtime-data (utanför repo)
    ├── tokens.enc               # Krypterade OAuth-tokens
    └── audit.log                # Audit-logg (append-only)
```

---

## Dataflöde – exempel: bokföra ett privat utlägg

```
Användare: "Bokför Telenor-faktura 450 kr för mars"
    │
    ▼
Claude aktiverar /project:book-expense
    │
    ▼
Ekonomirapportör (read-only) hämtar:
  - Senaste verifikationer för Telenor (kontroll mot dubbletter)
  - Aktuellt saldo på konto 2893 (skuld till ägare)
    │
    ▼
Validator beräknar verifikation:
  Debet  6212  360,00 SEK  (kostnad exkl. moms)
  Debet  2640   90,00 SEK  (ingående moms 25%)
  Kredit 2893  450,00 SEK  (skuld till ägare)
  Kontroll: 450 = 450 ✓
    │
    ▼
Claude presenterar förslaget för användaren
    │
    ▼
Användaren: "ja"
    │
    ▼
Bokförare (bookkeeper) anropar POST /vouchers
    │
    ▼
Audit-loggen uppdateras
    │
    ▼
Claude bekräftar med verifikationsnummer
```

---

## Teknologival

| Komponent | Val | Motivering |
|-----------|-----|------------|
| Språk | Python 3.12 | Mogna bibliotek för OAuth, krypto, HTTP |
| MCP-ramverk | `mcp` (Anthropic SDK) | Native integration med Claude |
| HTTP-klient | `httpx` | Async, enkel retry-hantering |
| Kryptering | `cryptography` (Fernet) | Välbeprövat, enkelt nyckelhantering |
| Nyckelring | `keyring` | Plattformsoberoende, systemnyckelring |
| Schemaläggning | `APScheduler` | Cron-syntax, persistent jobbstore |
| Testramverk | `pytest` | Standard, bra async-stöd |
