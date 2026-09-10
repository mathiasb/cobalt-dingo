package receipts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/receipts"
)

// exampleRouter builds a Router from the committed example rules — the file the
// real config is copied from, and therefore the thing worth asserting against.
func exampleRouter(t *testing.T) *receipts.Router {
	t.Helper()
	cfg, err := receipts.LoadConfig(repoPath(receipts.ExampleConfigPath))
	require.NoError(t, err)
	return receipts.NewRouter(cfg.RuleList(), cfg.DestinationList())
}

// Measured against 11 months of real mail on 2026-09-10: 16,558 messages, 143
// receipts Mathias had forwarded by hand, and 156 the rules would have
// forwarded — of which around 68 were not receipts at all.
//
// Every case below is a real subject from that run. Sender-only rules are the
// cause in each one: a merchant that sends receipts also sends review requests,
// verification codes, marketing and, in Söderberg's case, ordinary human mail
// about a private pension, which the rules routed into the company's supplier
// invoice inbox.
func TestExampleRules_doNotRouteTheseMeasuredFalsePositives(t *testing.T) {
	router := exampleRouter(t)

	cases := []struct{ name, from, subject string }{
		{"booking review request", "noreply@booking.com", "Näsby Slott is waiting for your review"},
		{"booking hotel message", "noreply@booking.com", "You have a message from Home Hotel Bilan"},
		{"booking hotel message sv", "noreply@booking.com", "Du har ett meddelande från Hotel Tenne"},
		{"booking rating request", "noreply@booking.com", "Rate Home Hotel Bilan"},
		{"booking rating request sv", "noreply@booking.com", "Betygsätt Mornington Victor Hotel London Belgravia"},
		{"booking marketing", "noreply@booking.com", "Reminder: Mathias, your Genius discount is ready to use"},
		{"booking security notice", "noreply@booking.com", "Important Booking.com Security Update"},
		{"booking verification code", "noreply-iam@booking.com", "Booking.com – H6QJWD är din verifieringskod"},
		{"booking preferences", "noreply@booking.com", "Updates to your communication preferences"},
		{"booking confirmation is not a receipt", "noreply@booking.com", "🛄 Thanks! Your booking is confirmed at Hotel Sonnenheim"},
		{"booking payment request", "noreply-payments@booking.com", "Please pay for your booking now"},
		{"booking upcoming charge", "noreply-payments@booking.com", "We’ll be charging your card soon"},
		{"booking invalid card", "noreply-invalidcc@booking.com", "Action required: Invalid credit card – confirm your payment details"},
		{"booking date change request", "customer.service@booking.com", "Din förfrågan om att ändra datum"},
		{"property stay information", "6925422649-vjpc@property.booking.com", "Information om din kommande vistelse"},
		{"property survey", "6925422649-vjpc@property.booking.com", "Hjälp oss att bli bättre!"},
		{"property thanks", "5947032482-y4mv@property.booking.com", "Tack för att du valde Näsby Slott!"},

		{"human mail about a private pension", "anders.ahlstrom@soderbergpartners.se", "Sv: Pension."},
		{"human mail about a meeting", "greger.bergenhem@soderbergpartners.se", "Sv: Dagens möte"},
		{"human mail with a name as subject", "greger.bergenhem@soderbergpartners.se", "mattias bergqvist"},
		{"post-meeting follow-up", "noreply@soderbergpartners.se", "Uppföljning efter mötet med Söderberg & Partners"},

		{"erp workflow prompt", "emeasupport@workforcelogiq.com", "Please enter your supplier invoice numbers for Definitely Consulting"},
		// Client name redacted: it appeared in the measured subject, and client
		// identities do not go into a repo.
		{"erp requisition notice", "notifications@workforcelogiq.com", "The requisition posted by <client> has been reopened"},

		{"vendor newsletter", "mistral-news@mistral.ai", "Action Required: Update Your Mistral AI Models"},

		{"uber marketing", "uber@uber.com", "Du har 100 kr i Uber Cash som väntar på ditt konto 💸"},
		{"uber discount push", "uber@uber.com", "Spara upp till 60% på ditt halloween-inköp"},
		{"uber eats marketing", "ubereats@uber.com", "Bus, Godis eller Deals? 👻"},
		{"uber device sign-in", "noreply@uber.com", "New device sign-in"},
		{"uber device sign-in sv", "noreply@uber.com", "Inloggning på ny enhet"},
		{"uber privacy notice", "noreply@uber.com", "We’ve updated our Privacy Notices"},
		{"uber reservation is not a receipt", "no-reply@uber.com", "Reservation confirmed for Tuesday, Mar 3"},

		{"telia order confirmation", "noreply@telia.se", "Här är din orderbekräftelse"},
		{"telia upsell", "noreply@telia.se", "Missa inte att komma igång med Netflix"},
		{"bahnhof order thanks", "noreply@bahnhof.se", "Tack för din beställning!"},
		{"bahnhof support ticket", "kundservice@bahnhof.se", "TID 785326 - Bahnhof"},

		{"easypark support ticket", "support@easypark.net", "Romme Alpin:: Ticket ID # 5171164"},
		{"easypark feedback request", "support@easypark.net", "Begäran [Romme Alpin] Dela din feedback med oss"},

		{"parkster deferred charge is not a receipt", "no-reply-charging@parkster.se", "Uppskjuten debitering"},
		{"google auto-topup notice", "payments-noreply@google.com", "Google Cloud Platform & APIs: Automatisk påfyllning är avstängd"},
		{"charge that has not happened yet", "hello@1password.com", "Your upcoming 1Password invoice (Mathias Bergqvist’s Family)."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dest, ok := router.Route(receipts.Mail{From: tc.from, Subject: tc.subject})
			if ok {
				t.Fatalf("routed to %q — this is not a receipt and forwarding it books a cost that does not exist", dest.Name)
			}
		})
	}
}

// The other half of the same measurement. These are subjects Mathias actually
// forwarded by hand, so a rule change that silences the false positives above
// must not silence these too — that trade is how a tightened rule set quietly
// stops collecting anything.
func TestExampleRules_stillRouteTheReceiptsHeForwardedByHand(t *testing.T) {
	router := exampleRouter(t)

	cases := []struct{ name, from, subject, dest string }{
		{"apple invoice sv", "no_reply@email.apple.com", "Din faktura från Apple", "mynt"},
		{"apple invoice en", "no_reply@email.apple.com", "Your invoice from Apple.", "mynt"},
		{"google workspace invoice", "payments-noreply@google.com", "Google Workspace: Din faktura är tillgänglig för d-ma.be", "mynt"},
		{"google cloud invoice", "payments-noreply@google.com", "Google Cloud Platform & APIs: Din faktura är tillgänglig", "mynt"},
		{"audible order", "donotreply@audible.com", "Thanks, your order is complete", "mynt"},
		{"uber trip", "noreply@uber.com", "[Personal] Your Friday morning trip with Uber", "mynt"},
		{"aimopark", "no-reply@aimopark.io", "Parkeringskvitto", "mynt"},
		{"sas refund", "no-reply@flysas.com", "SAS Refund Notice", "mynt"},
		{"asfinag", "shop@asfinag.at", "Your ASFINAG Webshop Invoice No. 400052877359", "mynt"},
		{"omio tickets", "service@omio.com", "Your tickets to Järvsö tågstation", "mynt"},
		{"mistral invoice", "no-reply@mistral.ai", "Din faktura från Mistral AI SAS #MSTRL-API-728586-002", "mynt"},
		{"github receipt", "noreply@github.com", "[GitHub] Payment Receipt for mathiasb", "mynt"},
		{"anthropic receipt", "invoice+statements@mail.anthropic.com", "Your receipt from Anthropic, PBC #2879-8484-5252", "mynt"},
		{"stripe on behalf of elevenlabs", "invoice+statements+acct_1M07hSLmdOdiMXBs@stripe.com", "Your receipt from Eleven Labs Inc. #2094-4757-2858", "mynt"},
		{"1password invoice", "hello@1password.com", "Your 1Password invoice (Mathias Bergqvist’s Family).", "mynt"},
		{"google play receipt", "googleplay-noreply@google.com", "Kvitto på din beställning från Google Play den 9 sep. 2026", "mynt"},
		{"hetzner invoice", "noreply.billing@hetzner.com", "Hetzner Online GmbH - Invoice 082001105251 (K0499337726)", "fortnox-invoices"},
		{"booking property bill", "6925422649-vjpc@property.booking.com", "Bill SE024B0043656 from Home Hotel Bilan", "mynt"},
		{"booking payment confirmation", "6925422649-vjpc@property.booking.com", "Din betalningsbekräftelse från Home Hotel Bilan", "mynt"},
		{"booking receipt", "5133046817-wggb@property.booking.com", "Kvitto & viktig bokningsinformation", "mynt"},
		{"easypark receipt", "no-reply@easypark.net", "Your EasyPark receipt", "mynt"},
		{"wolt purchase receipt", "info@wolt.com", "Purchase receipt", "fortnox-receipts"},
		{"wolt tip receipt", "info@wolt.com", "Tip Receipt", "fortnox-receipts"},
		{"stripe on behalf of neko health", "receipts+acct_1MEbglINImBQHjHQ@stripe.com", "Ditt kvitto från Neko Health [1867-2678]", "mynt"},
		{"sas e-ticket receipt", "no-reply@flysas.com", "Electronic Ticket Itinerary and Receipt from SAS - Booking 1234", "mynt"},
		{"sas refund confirmation", "no-reply@flysas.com", "Cancellation and refund confirmation", "mynt"},
		{"mistral payment receipt", "no-reply@mistral.ai", "Ditt betalningskvitto från Mistral AI SAS #MSTRL-API-728586", "mynt"},
		{"berget invoice", "andreas@berget.ai", "Faktura och kortbetalning", "mynt"},
		// Confirmed as company expenses by Mathias on 2026-09-10, when the
		// measurement surfaced them as judgment calls rather than errors.
		{"neko health via stripe", "receipts+acct_1MEbglINImBQHjHQ@stripe.com", "Ditt kvitto från Neko Health [1623-4394]", "mynt"},
		{"workspace invoice for another domain", "payments-noreply@google.com", "Google Workspace: Your invoice is available for another-domain.se", "mynt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dest, ok := router.Route(receipts.Mail{From: tc.from, Subject: tc.subject})
			require.True(t, ok, "a receipt he forwarded by hand must still route")
			assert.Equal(t, tc.dest, dest.Name)
		})
	}
}
