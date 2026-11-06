package addon_test

import (
	"testing"

	_ "github.com/invopop/gobl"
	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testInvoice is a coffee sold over the counter of a Prague café, paid in
// cash: the smallest invoice the addon accepts.
func testInvoice() *bill.Invoice {
	return &bill.Invoice{
		Regime:    tax.WithRegime("CZ"),
		Addons:    tax.WithAddons(addon.V1),
		Series:    "PRG",
		Code:      "0001",
		IssueDate: cal.MakeDate(2027, 1, 9),
		IssueTime: cal.NewTime(16, 45, 36),
		Currency:  currency.CZK,
		Tax: &bill.Tax{
			Ext: tax.ExtensionsOf(cbc.CodeMap{
				addon.ExtKeyUnit:   "303",
				addon.ExtKeyDevice: "POS-1",
			}),
		},
		Supplier: &org.Party{
			Name:  "Kavárna s.r.o.",
			TaxID: &tax.Identity{Country: "CZ", Code: "12345679"},
		},
		Lines: []*bill.Line{
			{
				Quantity: num.MakeAmount(1, 0),
				Item:     &org.Item{Name: "Káva", Price: num.NewAmount(6500, 2)},
				Taxes:    tax.Set{{Category: tax.CategoryVAT, Rate: tax.RateGeneral}},
			},
		},
	}
}

func TestInvoiceRules(t *testing.T) {
	tests := []struct {
		name   string
		change func(inv *bill.Invoice)
		err    string
	}{
		{
			name:   "valid sale",
			change: func(_ *bill.Invoice) {},
		},
		{
			name: "valid refund",
			change: func(inv *bill.Invoice) {
				inv.Type = bill.InvoiceTypeCreditNote
				inv.Preceding = []*org.DocumentRef{{Series: "PRG", Code: "0000"}}
			},
		},
		{
			name: "valid in EUR with a rate to CZK",
			change: func(inv *bill.Invoice) {
				inv.Currency = currency.EUR
				inv.ExchangeRates = []*currency.ExchangeRate{
					{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(2512, 2)},
				}
			},
		},
		{
			name:   "valid without series",
			change: func(inv *bill.Invoice) { inv.Series = "" },
		},
		{
			name:   "proforma",
			change: func(inv *bill.Invoice) { inv.Type = bill.InvoiceTypeProforma },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-01] ($.type) invoice type must be standard or credit-note",
		},
		{
			name:   "EUR without a rate",
			change: func(inv *bill.Invoice) { inv.Currency = currency.EUR },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-02] invoice must be in CZK or provide an exchange rate to CZK",
		},
		{
			name:   "no code",
			change: func(inv *bill.Invoice) { inv.Code = "" },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-03] ($.code) invoice code is required",
		},
		{
			name:   "sale number over 25 characters",
			change: func(inv *bill.Invoice) { inv.Series = "PRAGUE-OLD-TOWN-SQUARE" },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-04] invoice series and code joined with '/' must be at most 25",
		},
		{
			name:   "sale number with an accented letter",
			change: func(inv *bill.Invoice) { inv.Series = "BRNO-ŽABOVŘESKY" },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-04] invoice series and code joined with '/' must be at most 25",
		},
		{
			name:   "no issue time",
			change: func(inv *bill.Invoice) { inv.IssueTime = nil },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-05] ($.issue_time) issue time is required",
		},
		{
			name:   "no supplier tax ID",
			change: func(inv *bill.Invoice) { inv.Supplier.TaxID = nil },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-06] ($.supplier.tax_id) supplier tax ID is required",
		},
		{
			name: "German supplier",
			change: func(inv *bill.Invoice) {
				inv.Supplier.TaxID = &tax.Identity{Country: "DE", Code: "111111125"}
			},
			err: "[GOBL-CZ-EET-BILL-INVOICE-07] ($.supplier.tax_id.country) supplier tax ID must be Czech",
		},
		{
			name:   "supplier tax ID without code",
			change: func(inv *bill.Invoice) { inv.Supplier.TaxID.Code = "" },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-08] ($.supplier.tax_id.code) supplier tax ID code is required",
		},
		{
			name:   "no tax",
			change: func(inv *bill.Invoice) { inv.Tax = nil },
			err:    "[GOBL-CZ-EET-BILL-INVOICE-09] ($.tax) tax is required for the EET unit and device extensions",
		},
		{
			name: "no device",
			change: func(inv *bill.Invoice) {
				inv.Tax.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyUnit: "303"})
			},
			err: "[GOBL-CZ-EET-BILL-INVOICE-10] ($.tax.ext) tax extensions 'cz-eet-unit' and 'cz-eet-device' are required",
		},
		{
			name: "unit with a leading zero",
			change: func(inv *bill.Invoice) {
				inv.Tax.Ext = inv.Tax.Ext.Set(addon.ExtKeyUnit, "0303")
			},
			err: "[GOBL-CZ-EET-BILL-INVOICE-11] ($.tax.ext) tax extension 'cz-eet-unit' must be a number from 1 to 999999999",
		},
		{
			name: "device with an accented letter",
			change: func(inv *bill.Invoice) {
				inv.Tax.Ext = inv.Tax.Ext.Set(addon.ExtKeyDevice, "POKLADNA-Č1")
			},
			err: "[GOBL-CZ-EET-BILL-INVOICE-12] ($.tax.ext) tax extension 'cz-eet-device' must be 1 to 20 letters",
		},
		{
			name: "total over the EET limit",
			change: func(inv *bill.Invoice) {
				inv.Lines[0].Item.Price = num.NewAmount(8264462810, 2)
			},
			err: "[GOBL-CZ-EET-BILL-INVOICE-13] payable total must be at most 99,999,999.99 CZK",
		},
		{
			name: "total over the EET limit once converted to CZK",
			change: func(inv *bill.Invoice) {
				inv.Currency = currency.EUR
				inv.ExchangeRates = []*currency.ExchangeRate{
					{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(2512, 2)},
				}
				inv.Lines[0].Item.Price = num.NewAmount(330000000, 2)
			},
			err: "[GOBL-CZ-EET-BILL-INVOICE-13] payable total must be at most 99,999,999.99 CZK",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := testInvoice()
			tt.change(inv)
			require.NoError(t, inv.Calculate())
			err := rules.Validate(inv)
			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.err)
		})
	}
}

// The highest total EET takes, 99,999,999.99 CZK, passes.
func TestInvoiceRulesTotalAtTheLimit(t *testing.T) {
	inv := testInvoice()
	inv.Tax.PricesInclude = tax.CategoryVAT
	inv.Lines[0].Item.Price = num.NewAmount(9999999999, 2)
	require.NoError(t, inv.Calculate())
	require.Equal(t, "99999999.99", inv.Totals.Payable.String())
	assert.NoError(t, rules.Validate(inv))
}

func TestInvoiceRulesOnlyWithAddon(t *testing.T) {
	inv := testInvoice()
	inv.Addons = tax.WithAddons()
	inv.Tax.Ext = tax.Extensions{}
	inv.IssueTime = nil
	require.NoError(t, inv.Calculate())
	assert.NoError(t, rules.Validate(inv))
}

func TestSaleNumber(t *testing.T) {
	inv := testInvoice()
	assert.Equal(t, "PRG/0001", addon.SaleNumber(inv))
	inv.Series = ""
	assert.Equal(t, "0001", addon.SaleNumber(inv))
}
