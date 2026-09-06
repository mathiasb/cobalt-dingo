# Momsrapport – /project:vat-report

## Syfte
Sammanställ momsunderlag för ett kvartal och förbered för granskning inför Skatteverket.

## Indata
- Kvartal (Q1/Q2/Q3/Q4) och år – fråga om saknas, defaulta till innevarande kvartal

## Steg
1. Hämta alla verifikationer för perioden från Fortnox (läs-only)
2. Summera utgående moms per momskod (2610, 2611, 2612)
3. Summera ingående moms (2640, 2641, 2645)
4. Beräkna nettomoms (att betala eller återfå)
5. Lista de 10 största posterna i varje kategori för stickprovsgranskning
6. Kontrollera om periodens alla månader har verifikationer (varna om någon månad saknas)
7. Presentera komplett sammanfattning

## Output-format
```
## Momsrapport [KVARTAL] [ÅR]
Period: [DATUM]–[DATUM]
Deadline Skatteverket: [DATUM]

### Utgående moms (försäljning)
Momspliktig omsättning: [X] SEK
Moms 25%: [X] SEK

### Ingående moms (inköp)
Avdragsgill ingående moms: [X] SEK

### Sammanställning
Att betala / Återfå: [X] SEK

### Stickprov – största poster
[Tabell]
```

## Kritiska regler
- SKICKA ALDRIG till Skatteverket automatiskt – endast presentera underlag
- Påminn alltid om deadline
- Om nettomoms är ovanligt hög eller låg jämfört med föregående kvartal: flagga detta
