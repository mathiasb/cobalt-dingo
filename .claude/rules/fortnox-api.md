# Fortnox API – Konventioner och regler

## Autentisering
- Protokoll: OAuth 2.0 med Authorization Code Flow
- Tokens lagras krypterat på disk (aldrig i miljövariabler i klartext i kod)
- Access token-livslängd: 3600 sekunder – refresh sker automatiskt
- Scope-princip: Begär alltid minsta möjliga scope för uppgiften

## Bas-URL
```
https://api.fortnox.se/3/
```

## Viktiga endpoints för det här projektet
| Resurs           | Endpoint                  | Metoder       |
|-----------------|--------------------------|---------------|
| Fakturor         | /invoices                | GET, POST     |
| Verifikationer   | /vouchers                | GET, POST     |
| Kunder           | /customers               | GET           |
| Leverantörer     | /suppliers               | GET           |
| Konton (BAS)     | /accounts                | GET           |
| Anläggningar     | /assets                  | GET           |
| Momsrapport      | /vatreports              | GET           |

## Rate limiting
- Max 250 anrop/sekund per klient
- Vid 429: exponentiell backoff, max 3 försök
- Vid 503: retry efter 5 sekunder, max 2 försök

## Felhantering
- Logga alltid fullständigt svar vid fel (status, body, timestamp)
- Presentera fel på svenska för användaren
- Avbryt aldrig en pågående serie operationer tyst – rapportera och vänta

## Sandbox
- Använd Fortnox testmiljö under utveckling och för acceptanstester
- Sandbox-URL: Samma bas, men med testcredentials
- Markera tydligt i loggar när sandboxläge är aktivt
