# Säkerhetsregler

## Behörighetsnivåer
Tre nivåer gäller för alla operationer:

### Grön – kör automatiskt
- Läsa fakturor, verifikationer, konton, rapporter från Fortnox
- Hämta likviditetsöversikt och nyckeltal
- Generera utkast till verifikationer och rapporter (utan att spara)
- Skicka notifieringar och påminnelser till användaren

### Gul – kräver bekräftelse ("ja" i chatten)
- Skapa verifikationer i Fortnox
- Skapa eller uppdatera fakturor
- Initiera utbetalningar (skapar underlag, användaren godkänner i banken)
- Exportera data till externa system

### Röd – alltid blockerad
- Radera verifikationer eller fakturor
- Ändra kontoinställningar i Fortnox
- Skicka momsdeklaration till Skatteverket (genereras, aldrig skickas)
- Dela data med tredje part
- Läsa eller skriva OAuth-tokens utanför säker lagring

## Credentials-hantering
- OAuth-tokens lagras i krypterad fil: `~/.fn/tokens.enc`
- Krypteringsnyckel hämtas från systemnyckelring (keyring), aldrig hårdkodad
- Tokens loggas aldrig – inte ens maskerade versioner
- Client ID/Secret lagras i `.env`-fil som aldrig committas (finns i .gitignore)

## Audit-logg
- Varje API-anrop loggas: timestamp, endpoint, metod, statuskod, durationms
- Loggfil: `~/.fn/audit.log` – append-only, aldrig rensad automatiskt
- Loggformat: JSON Lines (ett JSON-objekt per rad)
- Känsliga parametrar (belopp, personnummer) loggas men maskeras i läsbart gränssnitt

## .gitignore – dessa filer committas ALDRIG
```
.env
.env.local
*.enc
audit.log
tokens/
__pycache__/
.pytest_cache/
```
