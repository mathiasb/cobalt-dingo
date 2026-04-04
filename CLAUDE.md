# Fortnox Agent – Projektinstruktioner

## Syfte
Det här projektet är ett agentiskt system som ger Claude säker, granskningsbar åtkomst
till en enskild firmas bokföring i Fortnox. Systemet bevakar, automatiserar och rapporterar
– men skriver aldrig till Fortnox utan explicit bekräftelse från användaren.

## Företagsprofil
- Bolagsform: Enskild firma
- Bransch: Konsulttjänster
- Kund: En aktiv kund (automatgenererade fakturor via portal)
- Kort: Mynt företagskort
- Fordon: Leasingbil (anläggningsregister)
- Utrustning: Två datorer (anläggningsregister)
- Återkommande utlägg: Telenor, Telia, Bahnhof (mobil + bredband, privat utlägg)
- Pensionsförvaltare: Söderberg & Partners

## Absoluta säkerhetsregler
1. ALLA läsoperationer mot Fortnox API är tillåtna utan bekräftelse
2. ALLA skrivoperationer kräver explicit "ja" från användaren i chatten
3. Skatteverket-underlag genereras och presenteras – skickas ALDRIG automatiskt
4. Varje API-anrop loggas i audit-loggen med timestamp, operation och parametrar
5. OAuth-tokens lagras aldrig i klartext i kod eller loggar

## Bokföringsregler (Sverige, enskild firma)
- Kontoplanen följer BAS 2024
- Momskoder: MP1 (25%), MP2 (12%), MP3 (6%), MF (momsfri)
- Momsperiod: Kvartal (standard för enskild firma under 40 MSEK)
- Skatteår: Kalenderår (jan–dec)
- Alla verifikationer måste vara balanserade (debet = kredit)
- Valuta: SEK om inget annat anges

## Återkommande utlägg – schema
| Leverantör | Typ       | Periodicitet | Konto | Momskod |
|------------|-----------|-------------|-------|---------|
| Telenor    | Mobil     | Månad        | 6212  | MP1     |
| Telia      | Bredband  | Månad        | 6212  | MP1     |
| Bahnhof    | Bredband  | Kvartal      | 6212  | MP1     |

## Konventioner
- Språk: Svenska i all kommunikation mot användaren
- Datumformat: YYYY-MM-DD
- Belopp: Alltid inkl. och exkl. moms redovisade separat
- Verifikationstext: Kortfattad men spårbar (leverantör + period)
- Commit-meddelanden: Engelska, conventional commits-format

## Arkitektur – kortfattad
Se `docs/architecture.md` för fullständig beskrivning.
- MCP-server: Exponerar verktyg mot Claude
- API-klient: OAuth2, retry, rate limiting mot Fortnox
- Validator: BAS-kontoplan, momsregler, balanscheck
- Audit-logg: Append-only, tidsstämplad
- Scheduler: Cron-baserad bevakning av deadlines och utlägg
