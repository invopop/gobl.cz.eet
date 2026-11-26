// Package addon provides the cz-eet-v1 GOBL addon for Czech EET 2.0 sales
// recording: the extensions that say where a sale happened, the tags and
// payment means key for prepaid credit and shared income, the stamp for the confirmation code, and the
// rules a payment must meet before it can be sent.
package addon

import (
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/norm"
	"github.com/invopop/gobl/pkg/here"
	_ "github.com/invopop/gobl/regimes/cz" // the regime a payment with the addon takes
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
	"github.com/invopop/gobl/tax"
)

const (
	// Key identifies the EET addon family, and names its rules so their
	// fault codes stay the same across versions.
	Key cbc.Key = "cz-eet"

	// V1 is the key for EET 2.0, schema version 4.
	V1 cbc.Key = Key + "-v1"
)

func init() {
	tax.RegisterAddonDef(newAddon())
	rules.RegisterWithGuard(
		Key.String(),
		rules.GOBL.Add("CZ-EET"),
		is.InContext(tax.AddonIn(V1)),
		billPaymentRules(),
	)
	norm.RegisterWithGuard(
		is.InContext(tax.AddonIn(V1)),
		norm.For(normalizePayment),
	)
}

func newAddon() *tax.AddonDef {
	return &tax.AddonDef{
		Key: V1,
		Name: i18n.String{
			i18n.EN: "Czech Republic EET 2.0",
			i18n.CS: "Česká republika EET 2.0",
		},
		Description: i18n.String{
			i18n.EN: here.Doc(`
				Support for EET 2.0 (evidence tržeb), the Czech sales-recording regime
				that applies from 1 January 2027. Payments taken in person (cash, card at
				the counter, vouchers and similar) are sent as signed data messages to the
				Financial Administration, which answers each with a confirmation code
				(POK).

				The message carries no lines, taxes or customer: only the merchant's tax
				ID, the recording unit and end device the payment was taken on, its
				sequence number and time, and the amount received in CZK. Every payment
				taken in person gets its own receipt with the addon.

				The payment's issue date and time are the moment the money was received,
				in Czech local time (Europe/Prague), which EET records with seconds.
			`),
		},
		Sources: []*cbc.Source{
			{
				Title: i18n.NewString("Finanční správa - Dokumenty k EET 2.0"),
				URL:   "https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0",
				At:    cal.NewDateTime(2026, 11, 6, 0, 0, 0),
			},
		},
		Tags:       []*tax.TagSet{paymentTags},
		Extensions: extensions,
		Stamps:     stamps,
	}
}
