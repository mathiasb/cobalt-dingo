# Skill: Övervaka inkorg och fakturor

## Trigger
Aktiveras automatiskt när Claude ser:
- Frågor om "vad har kommit in", "finns det något nytt", "daglig genomgång"
- Schemalagd körning (dagligen 08:00 via scheduler)
- Omnämnande av faktura, kvitto eller underlag

## Vad den gör
1. Söker i Gmail efter e-post från kända leverantörer (Telenor, Telia, Bahnhof, Mynt)
2. Identifierar fakturor och kvitton som kräver åtgärd
3. Kontrollerar om ny kundfaktura finns att importera från kundportal
4. Flaggar e-post från Fortnox, Skatteverket eller Söderberg & Partners
5. Sammanställer lista på åtgärdspunkter

## Output
Presenterar en kortfattad lista med vad som hittats och vad som kräver handling.
Frågar alltid om användaren vill agera på något direkt.

## Begränsningar
- Läser e-post, öppnar aldrig bilagor automatiskt
- Skapar aldrig verifikationer utan bekräftelse
- Raderar aldrig e-post
