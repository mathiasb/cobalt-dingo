# Daglig genomgång – /project:daily-briefing

## Syfte
Ge en komplett genomgång av vad som behöver uppmärksamhet idag och kommande dagar.
Körs proaktivt varje morgon eller när användaren frågar "vad behöver jag tänka på idag?"

## Steg
1. Hämta obetalda kundfakturor (förfallna eller förfaller inom 7 dagar)
2. Hämta obetalda leverantörsfakturor (förfallna eller förfaller inom 7 dagar)
3. Kontrollera om ny kundfaktura finns att hämta i kundportalen (baserat på schema)
4. Kontrollera kommande momsdeadlines (inom 30 dagar)
5. Kontrollera kommande utlägg att bokföra (baserat på schema i CLAUDE.md)
6. Hämta senaste Mynt-transaktioner utan kvitton
7. Sammanställ och presentera som strukturerad genomgång

## Output-format
```
## Genomgång [DATUM]

### Kräver åtgärd idag
- [Lista med konkreta uppgifter]

### Kommande denna vecka
- [Lista med uppgifter]

### Att hålla koll på
- [Påminnelser och deadlines]

### Likviditet
- Saldo: [X] SEK
- Förväntade inbetalningar: [X] SEK
- Förväntade utbetalningar: [X] SEK
- Prognos 30 dagar: [X] SEK
```

## Regler
- Läs-only – inga skrivoperationer i det här kommandot
- Om något kräver åtgärd: fråga om användaren vill hantera det nu
- Presentera alltid på svenska
