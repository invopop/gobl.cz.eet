// Package standalone_test checks the addon as an app that imports only it
// and the GOBL packages it names, without the root gobl package, which
// registers every regime. The eet package itself takes GOBL envelopes, so
// it imports the root package.
package standalone_test

import (
	"testing"

	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A payment without a currency or regime gets the Czech ones from its
// supplier's tax ID, and passes the addon's rules, as the addon registers
// the regime it needs.
func TestRegimeFromSupplier(t *testing.T) {
	pmt := &bill.Payment{
		Addons:    tax.WithAddons(addon.V1),
		Series:    "PRG",
		Code:      "000135",
		IssueDate: cal.MakeDate(2027, 1, 9),
		IssueTime: cal.NewTime(16, 45, 36),
		Ext: tax.ExtensionsOf(cbc.CodeMap{
			addon.ExtKeyUnit:   "303",
			addon.ExtKeyDevice: "POS-1",
		}),
		Supplier: &org.Party{
			Name:  "Kavárna u Mostu s.r.o.",
			TaxID: &tax.Identity{Country: "CZ", Code: "25596641"},
		},
		Lines:   []*bill.PaymentLine{{Amount: num.MakeAmount(20500, 2)}},
		Methods: []*pay.Record{{Key: pay.MeansKeyCash}},
	}
	require.NoError(t, pmt.Calculate())
	assert.Equal(t, currency.CZK, pmt.Currency)
	assert.Equal(t, "CZ", pmt.GetRegime().String())

	require.NoError(t, rules.Validate(pmt))
}
