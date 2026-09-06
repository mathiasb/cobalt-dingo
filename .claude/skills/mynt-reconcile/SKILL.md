# Skill: Mynt-avstämning och kvittohantering

## Trigger
Aktiveras automatiskt när Claude ser:
- Omnämnande av "Mynt", "kvitto", "företagskort"
- Frågor om transaktioner utan underlag
- Schemalagd veckovis körning (måndag 09:00)

## Vad den gör
1. Hämtar senaste Mynt-transaktioner (via Mynt API eller exportfil)
2. Identifierar transaktioner som saknar kvitto/underlag
3. Söker i Gmail efter matchande kvitton (baserat på datum, belopp, leverantör)
4. Föreslår matchningar för användaren att bekräfta
5. Loggar transaktioner som kräver manuellt kvitto

## Matchningslogik
- Datum: ±2 dagar från transaktion
- Belopp: Exakt match (inkl. moms)
- Leverantör: Fuzzy match på leverantörsnamn

## Output
Lista med tre kolumner:
- Automatiskt matchade (grön) – bekräfta batch
- Möjliga matchningar (gul) – granska och bekräfta en i taget  
- Saknar kvitto (röd) – kräver manuell hantering

## Vid saknat kvitto
Påminn användaren om att:
1. Kontrollera om kvitto finns som PDF i mail
2. Ladda upp manuellt till Mynt om det finns i pappersform
3. Logga undantaget om kvitto verkligen saknas (med motivering)

## Begränsningar
- Importerar aldrig kvitton till Mynt automatiskt
- Skapar aldrig verifikationer utan bekräftelse
