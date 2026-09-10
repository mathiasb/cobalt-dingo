# Receipt forwarding — review list, 2026-09-10

Every mail the collector **would** forward, from a dry run over 2025-10-01 →
2026-09-10 (16,558 messages, three accounts, All Mail so read and archived mail
included). Nothing has been forwarded.

Already-hand-forwarded receipts are excluded by the duplicate guard and are not
listed here. The 8 receipts the rules still MISS are in cobalt-dingo#78.

Sign-off gate: Mathias reviews this list, then production Fortnox OAuth (#50)
goes live, then the collector runs for real.

**24 candidates.**

| Account | Destination | Sender | Subject |
|---|---|---|---|
| mathias-bergqvist | mynt | `noreply@uber.com` | [Personal] Your Saturday morning trip with Uber |
| mathias-bergqvist | mynt | `6925422649-vjpc.bbjp.mvd5.qkkj@property.booking.com` | Din betalningsbekräftelse från Home Hotel Bilan |
| mathias-bergqvist | mynt | `6925422649-vjpc.bbjp.mvd5.qkkj@property.booking.com` | Din betalningsbekräftelse från Home Hotel Bilan |
| mathias-bergqvist | mynt | `6925422649-vjpc.bbjp.mvd5.qkkj@property.booking.com` | Bill SE024B0043744 from Home Hotel Bilan |
| mathias-bergqvist | mynt | `noreply@uber.com` | [Personal] Your Saturday evening trip with Uber |
| mathias-bergqvist | mynt | `no-reply@flysas.com` | Electronic Ticket Itinerary and Receipt from SAS - Booking... |
| mathias-bergqvist | mynt | `no-reply@flysas.com` | Your Receipt from SAS for Other Service |
| mathias-bergqvist | mynt | `no-reply@flysas.com` | Your Receipt from SAS for Other Service |
| mathias-bergqvist | mynt | `payments-noreply@google.com` | Google Workspace: Your invoice is available for fredandmat... |
| mathias-bergqvist | mynt | `5133046817-wggb.7mj6.fwwv.sswq@property.booking.com` | Kvitto & viktig bokningsinformation |
| mathias-bergqvist | mynt | `no-reply@flysas.com` | Cancellation and refund confirmation |
| mthbqv | mynt | `noreply@evernote.com` | Welcome to Evernote |
| mthbqv | mynt | `support-webform@evernote.com` | # 4284636 |
| mthbqv | mynt | `googleplay-noreply@google.com` | Kvitto på din beställning från Google Play den 9 nov. 2... |
| mthbqv | mynt | `googleplay-noreply@google.com` | Kvitto på din beställning från Google Play den 9 dec. 2... |
| mthbqv | mynt | `receipts+acct_1MEbglINImBQHjHQ@stripe.com` | Ditt kvitto från Neko Health [1867-2678] |
| mthbqv | mynt | `receipts+acct_1MEbglINImBQHjHQ@stripe.com` | Ditt kvitto från Neko Health [1623-4394] |
| mthbqv | fortnox-receipts | `info@wolt.com` | Purchase receipt |
| mthbqv | fortnox-receipts | `info@wolt.com` | Tip Receipt |
| dmabe | mynt | `no-reply@aimopark.io` | Parkeringskvitto |
| dmabe | mynt | `no-reply@easypark.net` | Your EasyPark receipt |
| dmabe | mynt | `andreas@berget.ai` | Faktura och kortbetalning |
| dmabe | fortnox-invoices | `billing@hetzner.com` | Hetzner Online GmbH - Invoice 081000972341 (K0499337726) |
| dmabe | mynt | `no-reply@mistral.ai` | Ditt betalningskvitto från Mistral AI SAS #MSTRL-API-7285... |

Reproduce: `RECEIPTS_SINCE=2025-10-01 RECEIPTS_MAX=40000 task receipts:dry-run`
