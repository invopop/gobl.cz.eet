package addon_test

import (
	"encoding/json"
	"math"
	"testing"

	_ "github.com/invopop/gobl"
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

// testPayment is a coffee paid in cash over the counter of a Prague café:
// the smallest payment the addon accepts.
func testPayment() *bill.Payment {
	return &bill.Payment{
		Regime:    tax.WithRegime("CZ"),
		Addons:    tax.WithAddons(addon.V1),
		Type:      bill.PaymentTypeReceipt,
		Series:    "PRG",
		Code:      "0001",
		IssueDate: cal.MakeDate(2027, 1, 9),
		IssueTime: cal.NewTime(16, 45, 36),
		Currency:  currency.CZK,
		Ext: tax.ExtensionsOf(cbc.CodeMap{
			addon.ExtKeyUnit:   "303",
			addon.ExtKeyDevice: "POS-1",
		}),
		Supplier: &org.Party{
			Name:  "Kavárna u Mostu s.r.o.",
			TaxID: &tax.Identity{Country: "CZ", Code: "25596641"},
		},
		Lines: []*bill.PaymentLine{
			{Amount: num.MakeAmount(6500, 2)},
		},
		Methods: []*pay.Record{
			{Key: pay.MeansKeyCash},
		},
	}
}

// inEUR moves the payment to EUR at 25.125 CZK.
func inEUR(pmt *bill.Payment) {
	pmt.Currency = currency.EUR
	pmt.ExchangeRates = []*currency.ExchangeRate{
		{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(25125, 3)},
	}
}

func TestPaymentRules(t *testing.T) {
	tests := []struct {
		name   string
		change func(pmt *bill.Payment)
		err    string
		// absent is a fault code that must not appear alongside err.
		absent string
	}{
		{
			name:   "valid sale",
			change: func(_ *bill.Payment) {},
		},
		{
			name: "valid refund",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Refund = true
			},
		},
		{
			name:   "valid in EUR with a rate to CZK",
			change: inEUR,
		},
		{
			name:   "valid without series",
			change: func(pmt *bill.Payment) { pmt.Series = "" },
		},
		{
			name: "valid with the method dated on the issue date",
			change: func(pmt *bill.Payment) {
				pmt.Methods[0].Date = cal.NewDate(2027, 1, 9)
			},
		},
		{
			name:   "request",
			change: func(pmt *bill.Payment) { pmt.Type = bill.PaymentTypeRequest },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-01] ($.type) payment type must be receipt",
		},
		{
			name:   "advice",
			change: func(pmt *bill.Payment) { pmt.Type = bill.PaymentTypeAdvice },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-01] ($.type) payment type must be receipt",
		},
		{
			name:   "EUR without a rate",
			change: func(pmt *bill.Payment) { pmt.Currency = currency.EUR },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-02] payment must be in CZK or provide an exchange rate to CZK",
			absent: "[GOBL-CZ-EET-BILL-PAYMENT-11]",
		},
		{
			name: "EUR with a rate to another currency only",
			change: func(pmt *bill.Payment) {
				pmt.Currency = currency.EUR
				pmt.ExchangeRates = []*currency.ExchangeRate{
					{From: currency.EUR, To: currency.USD, Amount: num.MakeAmount(108, 2)},
				}
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-02] payment must be in CZK or provide an exchange rate to CZK",
		},
		{
			name:   "no code",
			change: func(pmt *bill.Payment) { pmt.Series, pmt.Code = "", "" },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-03] ($.code) payment code is required",
			absent: "[GOBL-CZ-EET-BILL-PAYMENT-04]",
		},
		{
			name:   "sale number over 25 characters",
			change: func(pmt *bill.Payment) { pmt.Series = "PRAGUE-OLD-TOWN-SQUARE" },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-04] payment series and code joined with '/' must be at most 25",
		},
		{
			name:   "sale number with an accented letter",
			change: func(pmt *bill.Payment) { pmt.Series = "BRNO-ŽABOVŘESKY" },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-04] payment series and code joined with '/' must be at most 25",
		},
		{
			name:   "no issue time",
			change: func(pmt *bill.Payment) { pmt.IssueTime = nil },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-05] ($.issue_time) issue time is required",
		},
		{
			name:   "no supplier tax ID",
			change: func(pmt *bill.Payment) { pmt.Supplier.TaxID = nil },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-06] ($.supplier.tax_id) supplier tax ID with a code is required",
		},
		{
			name:   "supplier tax ID without code",
			change: func(pmt *bill.Payment) { pmt.Supplier.TaxID.Code = "" },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-06] ($.supplier.tax_id) supplier tax ID with a code is required",
		},
		{
			name: "German supplier",
			change: func(pmt *bill.Payment) {
				pmt.Supplier.TaxID = &tax.Identity{Country: "DE", Code: "111111125"}
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-07] ($.supplier.tax_id.country) supplier tax ID must be Czech",
		},
		{
			// A business customer paying by bank transfer: the payment
			// has the addon but no unit or device, as nothing was taken
			// in person.
			name:   "no extensions",
			change: func(pmt *bill.Payment) { pmt.Ext = tax.Extensions{} },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-08] ($.ext) extensions 'cz-eet-unit' and 'cz-eet-device' are required",
		},
		{
			name: "no unit",
			change: func(pmt *bill.Payment) {
				pmt.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyDevice: "POS-1"})
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-08] ($.ext) extensions 'cz-eet-unit' and 'cz-eet-device' are required",
		},
		{
			name: "no device",
			change: func(pmt *bill.Payment) {
				pmt.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyUnit: "303"})
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-08] ($.ext) extensions 'cz-eet-unit' and 'cz-eet-device' are required",
		},
		{
			name: "unit with a leading zero",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyUnit, "0303")
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-09] ($.ext) extension 'cz-eet-unit' must be a number from 1 to 999999999",
		},
		{
			name: "device with an accented letter",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyDevice, "POKLADNA-Č1")
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-10] ($.ext) extension 'cz-eet-device' must be 1 to 20 letters",
		},
		{
			name: "total over the EET limit",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Amount = num.MakeAmount(10000000000, 2)
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-11] payment total must be within ±99,999,999.99 CZK",
		},
		{
			name: "refund over the EET limit",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Refund = true
				pmt.Lines[0].Amount = num.MakeAmount(10000000000, 2)
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-11] payment total must be within ±99,999,999.99 CZK",
		},
		{
			name: "total over the EET limit once converted to CZK",
			change: func(pmt *bill.Payment) {
				inEUR(pmt)
				pmt.Lines[0].Amount = num.MakeAmount(400000000, 2)
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-11] payment total must be within ±99,999,999.99 CZK",
		},
		{
			// 100 paid by bank transfer the week before and 105 in cash at
			// the counter, sent as one document: only the cash is a sale,
			// and on its own day.
			name: "earlier transfer in the same document",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Amount = num.MakeAmount(20500, 2)
				pmt.Methods = []*pay.Record{
					{
						Key:    pay.MeansKeyCreditTransfer,
						Date:   cal.NewDate(2027, 1, 2),
						Amount: num.MakeAmount(10000, 2),
					},
					{Key: pay.MeansKeyCash, Amount: num.MakeAmount(10500, 2)},
				}
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-13] payment method date must be the payment's issue date",
		},
		{
			name: "second method dated the day after",
			change: func(pmt *bill.Payment) {
				pmt.Methods = []*pay.Record{
					{Key: pay.MeansKeyCash, Amount: num.MakeAmount(4000, 2)},
					{
						Key:    pay.MeansKeyCard,
						Date:   cal.NewDate(2027, 1, 10),
						Amount: num.MakeAmount(2500, 2),
					},
				}
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-13] payment method date must be the payment's issue date",
		},
		{
			// Nothing was received, so there is nothing to record.
			name: "zero",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Amount = num.MakeAmount(0, 2)
			},
			err: "[GOBL-BILL-PAYMENTLINE-04] ($.lines[0].amount) amount must be positive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pmt := testPayment()
			tt.change(pmt)
			require.NoError(t, pmt.Calculate())
			err := rules.Validate(pmt)
			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.err)
			if tt.absent != "" {
				assert.NotContains(t, err.Error(), tt.absent)
			}
		})
	}
}

func TestPaymentRulesTotalAtTheLimit(t *testing.T) {
	t.Run("in CZK", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Amount = num.MakeAmount(9999999999, 2)
		require.NoError(t, pmt.Calculate())
		assert.NoError(t, rules.Validate(pmt))
	})
	t.Run("refund in CZK", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Refund = true
		pmt.Lines[0].Amount = num.MakeAmount(9999999999, 2)
		require.NoError(t, pmt.Calculate())
		assert.NoError(t, rules.Validate(pmt))
	})
	t.Run("in EUR", func(t *testing.T) {
		// 3,980,099.50 EUR at 25.125 is 99,999,999.94 CZK.
		pmt := testPayment()
		inEUR(pmt)
		pmt.Lines[0].Amount = num.MakeAmount(398009950, 2)
		require.NoError(t, pmt.Calculate())
		require.Equal(t, "99999999.94", addon.SaleTotal(pmt).String())
		assert.NoError(t, rules.Validate(pmt))
	})
	t.Run("in JPY", func(t *testing.T) {
		// 661,375,660 JPY at 0.1512 is 99,999,999.79 CZK, within the
		// limit. Converted to whole CZK, as yen have no decimals, it
		// would be over.
		pmt := testPayment()
		pmt.Currency = currency.JPY
		pmt.ExchangeRates = []*currency.ExchangeRate{
			{From: currency.JPY, To: currency.CZK, Amount: num.MakeAmount(1512, 4)},
		}
		pmt.Lines[0].Amount = num.MakeAmount(661375660, 0)
		require.NoError(t, pmt.Calculate())
		assert.NoError(t, rules.Validate(pmt))
	})
}

func TestPaymentRulesOnlyWithAddon(t *testing.T) {
	pmt := testPayment()
	pmt.Addons = tax.WithAddons()
	pmt.Ext = tax.Extensions{}
	pmt.IssueTime = nil
	pmt.Type = bill.PaymentTypeRequest
	require.NoError(t, pmt.Calculate())
	assert.NoError(t, rules.Validate(pmt))
}

// Amounts written with fewer decimals than the currency has, such as 150
// for 150.00, add up to the same total in any order.
func TestAmountDecimals(t *testing.T) {
	tests := []struct {
		name  string
		lines []*bill.PaymentLine
		total string
	}{
		{
			name: "whole line, then a refund with cents",
			lines: []*bill.PaymentLine{
				{Amount: num.MakeAmount(150, 0)},
				{Refund: true, Amount: num.MakeAmount(9990, 2)},
			},
			total: "50.10",
		},
		{
			name: "refund with cents, then a whole line",
			lines: []*bill.PaymentLine{
				{Refund: true, Amount: num.MakeAmount(9990, 2)},
				{Amount: num.MakeAmount(150, 0)},
			},
			total: "50.10",
		},
		{
			name: "whole line, then cents",
			lines: []*bill.PaymentLine{
				{Amount: num.MakeAmount(100, 0)},
				{Amount: num.MakeAmount(50, 2)},
			},
			total: "100.50",
		},
		{
			name: "one decimal",
			lines: []*bill.PaymentLine{
				{Amount: num.MakeAmount(1005, 1)},
				{Amount: num.MakeAmount(5, 2)},
			},
			total: "100.55",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pmt := testPayment()
			pmt.Lines = tt.lines
			require.NoError(t, pmt.Calculate())
			require.NoError(t, rules.Validate(pmt))
			assert.Equal(t, tt.total, pmt.Total.String())
			assert.Equal(t, tt.total, pmt.Methods[0].Amount.String())
			assert.Equal(t, tt.total, addon.SaleTotal(pmt).String())
		})
	}
	t.Run("amounts as JSON numbers", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines = nil
		require.NoError(t, json.Unmarshal([]byte(`{"lines":[{"amount":150},{"refund":true,"amount":99.9}]}`), pmt))
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "50.10", addon.SaleTotal(pmt).String())
	})
	t.Run("without a currency yet", func(t *testing.T) {
		pmt := testPayment()
		pmt.Currency = ""
		pmt.Lines = []*bill.PaymentLine{
			{Amount: num.MakeAmount(150, 0)},
			{Refund: true, Amount: num.MakeAmount(9990, 2)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "50.10", addon.SaleTotal(pmt).String())
	})
	t.Run("method in another currency", func(t *testing.T) {
		// 10 EUR at 25.123 is 251.23 CZK: converted as 10, without
		// decimals, it would count as 251.00.
		pmt := testPayment()
		pmt.ExchangeRates = []*currency.ExchangeRate{
			{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(25123, 3)},
		}
		pmt.Lines = []*bill.PaymentLine{{Amount: num.MakeAmount(25123, 2)}}
		pmt.Methods = []*pay.Record{
			{Key: pay.MeansKeyCash, Currency: currency.EUR, Amount: num.MakeAmount(10, 0)},
		}
		require.NoError(t, pmt.Calculate())
		assert.Equal(t, "10.00", pmt.Methods[0].Amount.String())
		assert.NoError(t, rules.Validate(pmt))
	})
	t.Run("method in the payment's currency", func(t *testing.T) {
		pmt := testPayment()
		pmt.Methods = []*pay.Record{
			{Key: pay.MeansKeyCard, Amount: num.MakeAmount(60, 0)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(5, 0)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "60.00", pmt.Methods[0].Amount.String())
		assert.Equal(t, "5.00", pmt.Methods[1].Amount.String())
	})
	t.Run("more decimals than the currency are left for GOBL to refuse", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines = []*bill.PaymentLine{{Amount: num.MakeAmount(65125, 3)}}
		require.NoError(t, pmt.Calculate())
		assert.Equal(t, "65.125", pmt.Lines[0].Amount.String())
		assert.ErrorContains(t, rules.Validate(pmt), "[GOBL-BILL-PAYMENT-08] methods sum must match total")
	})
	t.Run("only with the addon", func(t *testing.T) {
		pmt := testPayment()
		pmt.Addons = tax.WithAddons()
		pmt.Lines = []*bill.PaymentLine{{Amount: num.MakeAmount(150, 0)}}
		require.NoError(t, pmt.Calculate())
		assert.Equal(t, "150", pmt.Lines[0].Amount.String())
	})
}

// A payment whose total isn't what its lines add up to would record
// another amount than the one received.
func TestPaymentTotalMatchesLines(t *testing.T) {
	const fault = "[GOBL-CZ-EET-BILL-PAYMENT-12] payment total must be the sum of its lines, with refund lines subtracted"
	t.Run("never calculated", func(t *testing.T) {
		pmt := testPayment()
		assert.ErrorContains(t, rules.Validate(pmt), fault)
	})
	t.Run("refund with a positive total", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Refund = true
		pmt.Total = num.MakeAmount(6500, 2)
		pmt.Methods[0].Amount = num.MakeAmount(6500, 2)
		assert.ErrorContains(t, rules.Validate(pmt), fault)
	})
	t.Run("total changed after calculating", func(t *testing.T) {
		pmt := testPayment()
		require.NoError(t, pmt.Calculate())
		pmt.Total = num.MakeAmount(6400, 2)
		pmt.Methods[0].Amount = num.MakeAmount(6400, 2)
		assert.ErrorContains(t, rules.Validate(pmt), fault)
	})
	t.Run("same value with other decimals", func(t *testing.T) {
		pmt := testPayment()
		require.NoError(t, pmt.Calculate())
		pmt.Total = num.MakeAmount(65, 0)
		assert.NoError(t, rules.Validate(pmt))
	})
}

func TestSaleNumber(t *testing.T) {
	pmt := testPayment()
	assert.Equal(t, "PRG/0001", addon.SaleNumber(pmt))
	pmt.Series = ""
	assert.Equal(t, "0001", addon.SaleNumber(pmt))
}

func TestSaleTotal(t *testing.T) {
	t.Run("sale in CZK", func(t *testing.T) {
		pmt := testPayment()
		require.NoError(t, pmt.Calculate())
		assert.Equal(t, "65.00", addon.SaleTotal(pmt).String())
	})
	t.Run("refund is negative", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Refund = true
		require.NoError(t, pmt.Calculate())
		assert.Equal(t, "-65.00", addon.SaleTotal(pmt).String())
	})
	t.Run("whole amount gets two decimals", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Amount = num.MakeAmount(65, 0)
		require.NoError(t, pmt.Calculate())
		assert.Equal(t, "65.00", addon.SaleTotal(pmt).String())
	})
	t.Run("converted to CZK", func(t *testing.T) {
		pmt := testPayment()
		pmt.Currency = currency.JPY
		pmt.ExchangeRates = []*currency.ExchangeRate{
			{From: currency.JPY, To: currency.CZK, Amount: num.MakeAmount(1512, 4)},
		}
		pmt.Lines[0].Amount = num.MakeAmount(661375660, 0)
		require.NoError(t, pmt.Calculate())
		assert.Equal(t, "99999999.79", addon.SaleTotal(pmt).String())
	})
	t.Run("no rate to CZK", func(t *testing.T) {
		pmt := testPayment()
		pmt.Currency = currency.EUR
		require.NoError(t, pmt.Calculate())
		assert.Nil(t, addon.SaleTotal(pmt))
	})
}

func TestPrepaidDrawn(t *testing.T) {
	prepaid := pay.MeansKeyOther.With(addon.MeansKeyPrepaid)
	t.Run("no method marked", func(t *testing.T) {
		pmt := testPayment()
		require.NoError(t, pmt.Calculate())
		assert.Nil(t, addon.PrepaidDrawn(pmt))
	})
	t.Run("marked methods added up", func(t *testing.T) {
		pmt := testPayment()
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Amount: num.MakeAmount(2000, 2)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(500, 2)},
			{Key: prepaid, Amount: num.MakeAmount(4000, 2)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "60.00", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("negative in a refund", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Refund = true
		pmt.Methods[0].Key = prepaid
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "-65.00", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("whole amount gets two decimals", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Amount = num.MakeAmount(65, 0)
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Amount: num.MakeAmount(65, 0)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "65.00", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("method in another currency", func(t *testing.T) {
		// 10.00 EUR of credit at 25.125 towards a payment in CZK.
		pmt := testPayment()
		pmt.ExchangeRates = []*currency.ExchangeRate{
			{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(25125, 3)},
		}
		pmt.Lines[0].Amount = num.MakeAmount(30000, 2)
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Currency: currency.EUR, Amount: num.MakeAmount(1000, 2)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(4875, 2)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "251.25", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("methods with different decimals, in either order", func(t *testing.T) {
		orders := [][]num.Amount{
			{num.MakeAmount(60, 0), num.MakeAmount(40, 2)},
			{num.MakeAmount(40, 2), num.MakeAmount(60, 0)},
		}
		for _, amounts := range orders {
			pmt := testPayment()
			pmt.Methods = []*pay.Record{
				{Key: prepaid, Amount: amounts[0]},
				{Key: prepaid, Amount: amounts[1]},
				{Key: pay.MeansKeyCash, Amount: num.MakeAmount(460, 2)},
			}
			require.NoError(t, pmt.Calculate())
			require.NoError(t, rules.Validate(pmt))
			assert.Equal(t, "60.40", addon.PrepaidDrawn(pmt).String(), amounts)
		}
	})
	t.Run("marked methods adding up to zero", func(t *testing.T) {
		pmt := testPayment()
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Amount: num.MakeAmount(5000, 2)},
			{Key: prepaid, Amount: num.MakeAmount(-5000, 2)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(6500, 2)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "0.00", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("method in another currency, written without decimals", func(t *testing.T) {
		// 10 EUR at 25.123 is 251.23 CZK, not 251.00.
		pmt := testPayment()
		pmt.ExchangeRates = []*currency.ExchangeRate{
			{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(25123, 3)},
		}
		pmt.Lines[0].Amount = num.MakeAmount(25123, 2)
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Currency: currency.EUR, Amount: num.MakeAmount(10, 0)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "251.23", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("method in a currency without decimals", func(t *testing.T) {
		// 1003 JPY at 0.1512 counts as 152.00 CZK, as GOBL's own check
		// that the methods add up to the total counts it.
		pmt := testPayment()
		pmt.ExchangeRates = []*currency.ExchangeRate{
			{From: currency.JPY, To: currency.CZK, Amount: num.MakeAmount(1512, 4)},
		}
		pmt.Lines[0].Amount = num.MakeAmount(30000, 2)
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Currency: currency.JPY, Amount: num.MakeAmount(1003, 0)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(14800, 2)},
		}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "152.00", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("marked method without a rate", func(t *testing.T) {
		// GOBL refuses the payment, as it can't add the methods up either.
		pmt := testPayment()
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Currency: currency.EUR, Amount: num.MakeAmount(100, 2)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(4000, 2)},
		}
		require.NoError(t, pmt.Calculate())
		assert.ErrorContains(t, rules.Validate(pmt), "[GOBL-BILL-PAYMENT-08] methods sum must match total")
		assert.Nil(t, addon.PrepaidDrawn(pmt))
	})
	t.Run("second marked method without a rate", func(t *testing.T) {
		pmt := testPayment()
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Amount: num.MakeAmount(2500, 2)},
			{Key: prepaid, Currency: currency.EUR, Amount: num.MakeAmount(100, 2)},
		}
		require.NoError(t, pmt.Calculate())
		assert.Nil(t, addon.PrepaidDrawn(pmt))
	})
	t.Run("total written without decimals", func(t *testing.T) {
		// Not calculated, so nothing has given the amounts their decimals.
		pmt := testPayment()
		pmt.Lines[0].Amount = num.MakeAmount(65, 0)
		pmt.Total = num.MakeAmount(65, 0)
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Amount: num.MakeAmount(6040, 2)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(460, 2)},
		}
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "60.40", addon.PrepaidDrawn(pmt).String())
	})
	t.Run("no rate to CZK", func(t *testing.T) {
		pmt := testPayment()
		pmt.Currency = currency.EUR
		pmt.Methods[0].Key = prepaid
		require.NoError(t, pmt.Calculate())
		assert.Nil(t, addon.PrepaidDrawn(pmt))
	})
}

func TestPrepaidDrawnLimit(t *testing.T) {
	prepaid := pay.MeansKeyOther.With(addon.MeansKeyPrepaid)
	const fault = "[GOBL-CZ-EET-BILL-PAYMENT-14] prepaid credit drawn must be within ±99,999,999.99 CZK"
	tests := []struct {
		name  string
		drawn num.Amount
		err   string
	}{
		{name: "at the limit", drawn: num.MakeAmount(9999999999, 2)},
		{name: "at the negative limit", drawn: num.MakeAmount(-9999999999, 2)},
		{name: "over the limit", drawn: num.MakeAmount(10000000000, 2), err: fault},
		{name: "over the negative limit", drawn: num.MakeAmount(-10000000000, 2), err: fault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Offset by cash, so the total of 50.00 stays within rule 11.
			pmt := testPayment()
			pmt.Lines[0].Amount = num.MakeAmount(5000, 2)
			pmt.Methods = []*pay.Record{
				{Key: prepaid, Amount: tt.drawn},
				{Key: pay.MeansKeyCash, Amount: num.MakeAmount(5000, 2).Subtract(tt.drawn)},
			}
			require.NoError(t, pmt.Calculate())
			err := rules.Validate(pmt)
			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.err)
		})
	}
	t.Run("over the limit once converted to CZK", func(t *testing.T) {
		// 4,000,000.00 EUR at 25.125 is 100,500,000.00 CZK.
		pmt := testPayment()
		inEUR(pmt)
		pmt.Lines[0].Amount = num.MakeAmount(5000, 2)
		pmt.Methods = []*pay.Record{
			{Key: prepaid, Amount: num.MakeAmount(400000000, 2)},
			{Key: pay.MeansKeyCash, Amount: num.MakeAmount(-399995000, 2)},
		}
		require.NoError(t, pmt.Calculate())
		assert.ErrorContains(t, rules.Validate(pmt), fault)
	})
}

func TestPrepaidMeansKey(t *testing.T) {
	for _, key := range []cbc.Key{"other+prepaid", "card+prepaid"} {
		t.Run(key.String(), func(t *testing.T) {
			pmt := testPayment()
			pmt.Methods[0].Key = key
			require.NoError(t, pmt.Calculate())
			assert.NoError(t, rules.Validate(pmt))
			assert.Equal(t, pmt.Total.String(), addon.PrepaidDrawn(pmt).String())
		})
	}
}

// loaded marks every line of the payment as a load of prepaid credit.
func loaded(pmt *bill.Payment) {
	for _, l := range pmt.Lines {
		l.Ext = l.Ext.Set(addon.ExtKeyPrepaidLoad, addon.ExtCodeTrue)
	}
}

// loadLine is a payment line that loads prepaid credit.
func loadLine(amount num.Amount, refund bool) *bill.PaymentLine {
	return &bill.PaymentLine{
		Refund: refund,
		Amount: amount,
		Ext:    tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyPrepaidLoad: addon.ExtCodeTrue}),
	}
}

func TestPrepaidLoaded(t *testing.T) {
	t.Run("not marked", func(t *testing.T) {
		pmt := testPayment()
		require.NoError(t, pmt.Calculate())
		assert.Nil(t, addon.PrepaidLoaded(pmt))
	})
	t.Run("credit loaded", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines = []*bill.PaymentLine{loadLine(num.MakeAmount(100000, 2), false)}
		pmt.Methods[0].Amount = num.Amount{}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "1000.00", addon.PrepaidLoaded(pmt).String())
	})
	t.Run("load paid back", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines = []*bill.PaymentLine{loadLine(num.MakeAmount(6500, 2), true)}
		pmt.Methods[0].Amount = num.Amount{}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "-65.00", addon.PrepaidLoaded(pmt).String())
	})
	t.Run("credit loaded with a purchase", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines = []*bill.PaymentLine{
			loadLine(num.MakeAmount(100000, 2), false),
			{Amount: num.MakeAmount(5000, 2)},
		}
		pmt.Methods[0].Amount = num.Amount{}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "1050.00", pmt.Total.String())
		assert.Equal(t, "1000.00", addon.PrepaidLoaded(pmt).String())
	})
	t.Run("lines marked false", func(t *testing.T) {
		pmt := testPayment()
		pmt.Lines[0].Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyPrepaidLoad: addon.ExtCodeFalse})
		require.NoError(t, pmt.Calculate())
		assert.Nil(t, addon.PrepaidLoaded(pmt))
	})
	t.Run("converted to CZK", func(t *testing.T) {
		pmt := testPayment()
		inEUR(pmt)
		pmt.Lines = []*bill.PaymentLine{loadLine(num.MakeAmount(1000, 2), false)}
		pmt.Methods[0].Amount = num.Amount{}
		require.NoError(t, pmt.Calculate())
		require.NoError(t, rules.Validate(pmt))
		assert.Equal(t, "251.25", addon.PrepaidLoaded(pmt).String())
	})
	t.Run("no rate to CZK", func(t *testing.T) {
		pmt := testPayment()
		pmt.Currency = currency.EUR
		loaded(pmt)
		require.NoError(t, pmt.Calculate())
		assert.Nil(t, addon.PrepaidLoaded(pmt))
	})
}

func TestPrepaidLoadRules(t *testing.T) {
	prepaid := pay.MeansKeyOther.With(addon.MeansKeyPrepaid)
	tests := []struct {
		name   string
		change func(pmt *bill.Payment)
		err    string
	}{
		{name: "load on a line", change: loaded},
		{
			name: "load marked false",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyPrepaidLoad: addon.ExtCodeFalse})
			},
		},
		{
			name: "load marked with another code",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyPrepaidLoad: "yes"})
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-19] ($.lines[0].ext) extension 'cz-eet-prepaid-load' must be 'true' or 'false'",
		},
		{
			name: "load marked on the payment",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyPrepaidLoad, addon.ExtCodeTrue)
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-18] ($.ext) extension 'cz-eet-prepaid-load' goes in the payment lines",
		},
		{
			name: "multiple taxpayers marked on a line",
			change: func(pmt *bill.Payment) {
				pmt.Lines[0].Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyMultipleTaxpayers: addon.ExtCodeTrue})
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-20] ($.lines[0].ext) extension 'cz-eet-multiple-taxpayers' goes in the payment's extensions",
		},
		{
			// The total fits, as a refund line takes the load back down.
			name: "load over the limit",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{
					loadLine(num.MakeAmount(10000000000, 2), false),
					{Refund: true, Amount: num.MakeAmount(9999999000, 2)},
				}
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-21] prepaid credit loaded must be within ±99,999,999.99 CZK (EET urceno_cerp_zuct)",
		},
		{
			// Nothing in the item description keeps a load from being paid
			// from credit, so both go in one message.
			name: "load paid from prepaid credit",
			change: func(pmt *bill.Payment) {
				loaded(pmt)
				pmt.Methods[0].Key = prepaid
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pmt := testPayment()
			tt.change(pmt)
			pmt.Methods[0].Amount = num.Amount{}
			require.NoError(t, pmt.Calculate())
			err := rules.Validate(pmt)
			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.err)
		})
	}
}

// delegated makes the payment one recorded by the ordering issuer on the
// supplier's behalf.
func delegated(pmt *bill.Payment) {
	pmt.Ordering = &bill.Ordering{Issuer: &org.Party{
		Name:  "Čerpací stanice Brumlík s.r.o.",
		TaxID: &tax.Identity{Country: "CZ", Code: "36055514"},
	}}
}

// sharedIncome marks the payment as income of several taxpayers.
func sharedIncome(pmt *bill.Payment) {
	pmt.Ext = pmt.Ext.Set(addon.ExtKeyMultipleTaxpayers, addon.ExtCodeTrue)
}

func TestDelegationRules(t *testing.T) {
	tests := []struct {
		name   string
		change func(pmt *bill.Payment)
		err    string
	}{
		{name: "one taxpayer, with an issuer", change: delegated},
		{
			name: "many taxpayers, with an issuer",
			change: func(pmt *bill.Payment) {
				delegated(pmt)
				sharedIncome(pmt)
			},
		},
		{
			// The payee receives the money and isn't in the message.
			name: "payee without the extension",
			change: func(pmt *bill.Payment) {
				pmt.Payee = &org.Party{Name: "Pokladní služby s.r.o.", TaxID: &tax.Identity{Country: "DE", Code: "111111125"}}
			},
		},
		{name: "many taxpayers, without an issuer", change: sharedIncome},
		{
			name: "one taxpayer, marked",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyMultipleTaxpayers, addon.ExtCodeFalse)
			},
		},
		{
			name: "many taxpayers marked with another code",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyMultipleTaxpayers, "1")
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-17] ($.ext) extension 'cz-eet-multiple-taxpayers' must be 'true' or 'false' (EET povereni_vice_popl)",
		},
		{
			name: "issuer without a tax ID",
			change: func(pmt *bill.Payment) {
				delegated(pmt)
				pmt.Ordering.Issuer.TaxID = nil
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-15] ($.ordering.issuer.tax_id) ordering issuer tax ID with a code is required (EET eic_popl)",
		},
		{
			name: "issuer tax ID without a code",
			change: func(pmt *bill.Payment) {
				delegated(pmt)
				pmt.Ordering.Issuer.TaxID.Code = ""
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-15] ($.ordering.issuer.tax_id) ordering issuer tax ID with a code is required (EET eic_popl)",
		},
		{
			name: "German issuer",
			change: func(pmt *bill.Payment) {
				delegated(pmt)
				pmt.Ordering.Issuer.TaxID = &tax.Identity{Country: "DE", Code: "111111125"}
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-16] ($.ordering.issuer.tax_id.country) ordering issuer tax ID must be Czech (EET eic_popl)",
		},
		{
			// The service takes it with warning 7 of the interface
			// description.
			name: "issuer is the supplier",
			change: func(pmt *bill.Payment) {
				delegated(pmt)
				pmt.Ordering.Issuer.TaxID = &tax.Identity{Country: "CZ", Code: pmt.Supplier.TaxID.Code}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pmt := testPayment()
			tt.change(pmt)
			require.NoError(t, pmt.Calculate())
			err := rules.Validate(pmt)
			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.err)
		})
	}
}

// A nil entry among the exchange rates counts as no rate: the amounts come
// back nil and rule 02 refuses the payment, with no panic.
func TestNilExchangeRate(t *testing.T) {
	for name, rates := range map[string][]*currency.ExchangeRate{
		"first": {nil, {From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(2500, 2)}},
		"last":  {{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(2500, 2)}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			pmt := testPayment()
			inEUR(pmt)
			pmt.Methods[0].Key = pay.MeansKeyOther.With(addon.MeansKeyPrepaid)
			require.NoError(t, pmt.Calculate())
			pmt.ExchangeRates = rates
			assert.NotPanics(t, func() {
				assert.Nil(t, addon.SaleTotal(pmt))
				assert.Nil(t, addon.PrepaidDrawn(pmt))
				loaded(pmt)
				assert.Nil(t, addon.PrepaidLoaded(pmt))
				assert.ErrorContains(t, rules.Validate(pmt), "[GOBL-CZ-EET-BILL-PAYMENT-02]")
			})
		})
	}
}

// The smallest int64 stays negative when negated, so the limits are checked
// on both sides: as a total and as the part drawn, it is refused.
func TestAmountLimitsWithTheSmallestCoefficient(t *testing.T) {
	smallest := num.MakeAmount(math.MinInt64, 2)
	t.Run("total", func(t *testing.T) {
		pmt := testPayment()
		require.NoError(t, pmt.Calculate())
		pmt.Total = smallest
		assert.ErrorContains(t, rules.Validate(pmt), "[GOBL-CZ-EET-BILL-PAYMENT-11] payment total must be within ±99,999,999.99 CZK")
	})
	t.Run("drawn", func(t *testing.T) {
		pmt := testPayment()
		pmt.Methods[0].Key = pay.MeansKeyOther.With(addon.MeansKeyPrepaid)
		require.NoError(t, pmt.Calculate())
		pmt.Methods[0].Amount = smallest
		assert.ErrorContains(t, rules.Validate(pmt), "[GOBL-CZ-EET-BILL-PAYMENT-14] prepaid credit drawn must be within ±99,999,999.99 CZK")
	})
}
