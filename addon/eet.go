// Package addon provides the cz-eet-v1 GOBL addon for Czech EET 2.0 sales
// recording: the extensions that identify where a sale happened, the stamp
// for the confirmation code, and the rules an invoice must meet before it
// can be sent.
package addon

import (
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/pkg/here"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
	"github.com/invopop/gobl/tax"
)

const (
	// Key identifies the EET addon family. Individual versions append a
	// suffix; the family key is used as the fault-code namespace so that
	// rules that carry across versions keep stable codes.
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
		billInvoiceRules(),
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
				that applies from 1 January 2027. Each sale paid in person (cash, card at
				the counter, vouchers and similar) is sent as a signed data message to the
				Financial Administration, which answers with a confirmation code (POK).

				The message carries no lines, taxes or customer: only the merchant's tax
				ID, the recording unit and end device the sale happened on, the sale's
				sequence number and time, and the total paid in CZK. This addon adds the
				unit and device extensions, the POK stamp, and the rules a simplified or
				standard invoice must meet so that the message can be built from it.
			`),
		},
		Sources: []*cbc.Source{
			{
				Title: i18n.NewString("Finanční správa - Dokumenty k EET 2.0"),
				URL:   "https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0",
				At:    cal.NewDateTime(2026, 11, 6, 0, 0, 0),
			},
		},
		Extensions: extensions,
		Stamps:     stamps,
	}
}
