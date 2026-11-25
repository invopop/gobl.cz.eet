package addon_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/invopop/gobl"
	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The exported helpers take a nil payment as nothing to work on.
func TestHelpersWithNilPayment(t *testing.T) {
	assert.NotPanics(t, func() {
		assert.Nil(t, addon.SaleTotal(nil))
		assert.Nil(t, addon.PrepaidDrawn(nil))
		assert.Nil(t, addon.PrepaidLoaded(nil))
		assert.Empty(t, addon.SaleNumber(nil))
	})
}

// Nil entries and missing parts in a payment are faults or are skipped,
// never a panic, in the normalizer, the rules and the helpers, with the
// payment calculated or not.
func TestPaymentsWithNilParts(t *testing.T) {
	changes := map[string]func(*bill.Payment){
		"a nil line":               func(p *bill.Payment) { p.Lines = append(p.Lines, nil) },
		"first line nil":           func(p *bill.Payment) { p.Lines[0] = nil },
		"line without a document":  func(p *bill.Payment) { p.Lines[0].Document = nil },
		"line with an empty one":   func(p *bill.Payment) { p.Lines[0].Document = &org.DocumentRef{} },
		"no lines":                 func(p *bill.Payment) { p.Lines = nil },
		"a nil method":             func(p *bill.Payment) { p.Methods = append(p.Methods, nil) },
		"first method nil":         func(p *bill.Payment) { p.Methods[0] = nil },
		"an empty method":          func(p *bill.Payment) { p.Methods[0] = &pay.Record{} },
		"no methods":               func(p *bill.Payment) { p.Methods = nil },
		"no supplier":              func(p *bill.Payment) { p.Supplier = nil },
		"supplier without tax ID":  func(p *bill.Payment) { p.Supplier.TaxID = nil },
		"no issue time":            func(p *bill.Payment) { p.IssueTime = nil },
		"payee without tax ID":     func(p *bill.Payment) { p.Payee = &org.Party{Name: "Petr Svoboda"} },
		"issuer without tax ID":    func(p *bill.Payment) { p.Ordering = &bill.Ordering{Issuer: &org.Party{Name: "Petr Svoboda"}} },
		"ordering without issuer":  func(p *bill.Payment) { p.Ordering = &bill.Ordering{} },
		"empty extensions":         func(p *bill.Payment) { p.Ext = tax.Extensions{} },
		"method, empty extensions": func(p *bill.Payment) { p.Methods[0].Ext = tax.Extensions{} },
	}
	examples := []string{"receipt", "refund", "prepaid-draw", "prepaid-load", "delegated-fuel", "delegated-parking", "delegated-craftsmen"}
	for name, change := range changes {
		for _, example := range examples {
			for _, calculate := range []bool{false, true} {
				pmt := loadPayment(t, example)
				assert.NotPanics(t, func() {
					change(pmt)
					if calculate {
						_ = pmt.Calculate()
					}
					_ = rules.Validate(pmt)
					addon.SaleTotal(pmt)
					addon.PrepaidDrawn(pmt)
					addon.PrepaidLoaded(pmt)
					addon.SaleNumber(pmt)
				}, "%s, %s, calculated %v", name, example, calculate)
			}
		}
	}
	t.Run("almost empty payments", func(t *testing.T) {
		for _, pmt := range []*bill.Payment{
			{},
			{Addons: tax.WithAddons(addon.V1)},
			{Addons: tax.WithAddons(addon.V1), Supplier: &org.Party{}, Payee: &org.Party{}},
			{Addons: tax.WithAddons(addon.V1), Ordering: &bill.Ordering{Issuer: &org.Party{}}},
			{Addons: tax.WithAddons(addon.V1), Lines: []*bill.PaymentLine{nil, {}}, Methods: []*pay.Record{nil, {}}},
		} {
			assert.NotPanics(t, func() {
				_ = pmt.Calculate()
				_ = rules.Validate(pmt)
			})
		}
	})
}

func loadPayment(t *testing.T, name string) *bill.Payment {
	t.Helper()
	data, err := os.ReadFile("../examples/out/" + name + ".json")
	require.NoError(t, err)
	env := new(gobl.Envelope)
	require.NoError(t, json.Unmarshal(data, env))
	pmt, ok := env.Extract().(*bill.Payment)
	require.True(t, ok)
	return pmt
}
