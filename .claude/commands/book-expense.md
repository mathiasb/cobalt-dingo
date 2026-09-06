# Bokför utlägg – /project:book-expense

## Syfte
Guida användaren genom att bokföra ett privat utlägg korrekt och skapa verifikation i Fortnox.

## Indata (fråga om saknas)
- Leverantör
- Belopp (inkl. moms)
- Datum
- Typ av kostnad (telecom, resor, logi, övrigt)
- Finns kvitto/underlag?

## Steg
1. Identifiera rätt BAS-konto baserat på kostnadstyp (se rules/swedish-vat.md)
2. Beräkna moms (kontrollera momssats mot leverantörstyp)
3. Bygg verifikationsförslag:
   - Debet: Kostnadskonto (exkl. moms)
   - Debet: 2640 Ingående moms
   - Kredit: 2893 Skuld till närstående (inkl. moms)
4. Validera att debet = kredit
5. Presentera förslaget tydligt för användaren
6. Invänta bekräftelse ("ja") innan något skrivs till Fortnox
7. Skapa verifikation via API
8. Logga i audit-loggen
9. Fråga om användaren vill hantera utbetalning nu eller samla ihop

## Felhantering
- Om kvitto saknas: påminn om att kvitto krävs för momsavdrag, men fortsätt om användaren bekräftar
- Om BAS-konto är oklart: presentera alternativ och fråga
- Om API-anrop misslyckas: visa fel, försök inte automatiskt igen
