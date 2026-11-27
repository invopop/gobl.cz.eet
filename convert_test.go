package eet

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/invopop/gobl"
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
	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium/xsd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testOptions fix the message UUID, which NewSale takes at random.
var testOptions = generateOptions{uuid: "8c1a7ad4-5f3e-4b2a-9d61-0e7f3c2b1a90"}

// testSale builds a sale with the send time Sign would set, fixed.
func testSale(pmt *bill.Payment, o generateOptions, sent time.Time) (*Sale, error) {
	sale, err := newSale(pmt, o)
	if err != nil {
		return nil, err
	}
	sale.Header.SentAt = sentAt(sent)
	return sale, nil
}

// testNow is the send time of the tests' sales.
var testNow = time.Date(2027, 1, 9, 15, 45, 37, 0, time.UTC)

func TestNewSaleExamples(t *testing.T) {
	for _, name := range []string{"receipt", "refund", "prepaid-draw", "prepaid-load", "prepaid-load-with-purchase", "delegated-fuel", "delegated-parking", "delegated-craftsmen", "exchange"} {
		t.Run(name, func(t *testing.T) {
			// Sent a second after the sale, as a till would send it.
			pmt := loadExample(t, name)
			sale, err := testSale(pmt, testOptions, soldAt(pmt).Add(time.Second))
			require.NoError(t, err)
			data := saleXML(t, sale)
			validateSchema(t, data)

			golden := "examples/out/" + name + ".xml"
			if *update {
				require.NoError(t, os.WriteFile(golden, data, 0o644))
			}
			expected, err := os.ReadFile(golden)
			require.NoError(t, err, "run the tests with -update to create it")
			assert.Equal(t, string(expected), string(data))
		})
	}
}

// amount reads a test amount, such as "205.00".
func amount(value string) num.Amount {
	a, err := num.AmountFromString(value)
	if err != nil {
		panic(err)
	}
	return a
}

// paymentLine is a payment line for an amount in the payment's currency.
func paymentLine(value string, refund bool) *bill.PaymentLine {
	return &bill.PaymentLine{Refund: refund, Amount: amount(value)}
}

// loadLine is a payment line that loads prepaid credit.
func loadLine(value string, refund bool) *bill.PaymentLine {
	line := paymentLine(value, refund)
	line.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyPrepaidLoad: addon.ExtCodeTrue})
	return line
}

// paidWith is a payment method for an amount in the payment's currency.
func paidWith(key cbc.Key, value string) *pay.Record {
	return &pay.Record{Key: key, Amount: amount(value)}
}

// drawn is a payment method that draws on prepaid credit. An empty amount
// is left for GOBL to fill from the total.
func drawn(value string) *pay.Record {
	m := &pay.Record{Key: pay.MeansKeyOther.With(addon.MeansKeyPrepaid)}
	if value != "" {
		m.Amount = amount(value)
	}
	return m
}

// paidInCash leaves the cash amount for GOBL to fill from the total.
func paidInCash(pmt *bill.Payment) {
	pmt.Methods = []*pay.Record{{Key: pay.MeansKeyCash}}
}

// inEUR moves the payment to EUR at 25.125 CZK.
func inEUR(pmt *bill.Payment) {
	pmt.Currency = currency.EUR
	pmt.ExchangeRates = []*currency.ExchangeRate{
		{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(25125, 3)},
	}
}

func TestNewSaleData(t *testing.T) {
	tests := []struct {
		name   string
		change func(pmt *bill.Payment)
		check  func(t *testing.T, data *Data)
	}{
		{
			name: "sale",
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, &Data{
					TaxID:    "CZ25596641",
					UnitID:   "303",
					DeviceID: "POS-1",
					Number:   "PRG/000135",
					SoldAt:   "2027-01-09T16:45:36+01:00",
					Total:    "205.00",
				}, data)
			},
		},
		{
			name: "summer time",
			change: func(pmt *bill.Payment) {
				pmt.IssueDate = cal.MakeDate(2027, 7, 1)
				pmt.IssueTime = cal.NewTime(12, 0, 5)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "2027-07-01T12:00:05+02:00", data.SoldAt)
			},
		},
		{
			// 02:30 happens twice that night: the first time, in summer time,
			// is the one every attempt sends.
			name: "hour repeated when summer time ends",
			change: func(pmt *bill.Payment) {
				pmt.IssueDate = cal.MakeDate(2027, 10, 31)
				pmt.IssueTime = cal.NewTime(2, 30, 0)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "2027-10-31T02:30:00+02:00", data.SoldAt)
			},
		},
		{
			name: "last second of the repeated hour",
			change: func(pmt *bill.Payment) {
				pmt.IssueDate = cal.MakeDate(2027, 10, 31)
				pmt.IssueTime = cal.NewTime(2, 59, 59)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "2027-10-31T02:59:59+02:00", data.SoldAt)
			},
		},
		{
			name: "after the repeated hour",
			change: func(pmt *bill.Payment) {
				pmt.IssueDate = cal.MakeDate(2027, 10, 31)
				pmt.IssueTime = cal.NewTime(3, 0, 0)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "2027-10-31T03:00:00+01:00", data.SoldAt)
			},
		},
		{
			// 02:30 never happens that night: clocks go from 02:00 to 03:00.
			name: "hour skipped when summer time starts",
			change: func(pmt *bill.Payment) {
				pmt.IssueDate = cal.MakeDate(2027, 3, 28)
				pmt.IssueTime = cal.NewTime(2, 30, 0)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "2027-03-28T03:30:00+02:00", data.SoldAt)
			},
		},
		{
			name:   "no series",
			change: func(pmt *bill.Payment) { pmt.Series = "" },
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "000135", data.Number)
			},
		},
		{
			name: "refund",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("99.90", true)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "-99.90", data.Total)
			},
		},
		{
			// A wrong item taken back and the right one sold: the
			// customer pays the difference.
			name: "correction",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("100.00", true), paymentLine("150.00", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "50.00", data.Total)
			},
		},
		{
			// An item exchanged for another of the same price.
			name: "even exchange",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("100.00", true), paymentLine("100.00", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "0.00", data.Total)
			},
		},
		{
			// The rest of an invoice after a deposit: only what is paid
			// now is the sale.
			name: "final payment",
			change: func(pmt *bill.Payment) {
				l := paymentLine("250.00", false)
				l.Document = &org.DocumentRef{Series: "FV", Code: "2027-0042"}
				l.Payable = num.NewAmount(125000, 2)
				l.Advances = num.NewAmount(100000, 2)
				pmt.Lines = []*bill.PaymentLine{l}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "250.00", data.Total)
			},
		},
		{
			// An invoice of 1,250.00 paid partly in cash; the rest comes by
			// bank transfer, which gets no receipt with the addon.
			name: "part of an invoice in person",
			change: func(pmt *bill.Payment) {
				l := paymentLine("250.00", false)
				l.Document = &org.DocumentRef{Series: "FV", Code: "2027-0042"}
				l.Payable = num.NewAmount(125000, 2)
				pmt.Lines = []*bill.PaymentLine{l}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "250.00", data.Total)
			},
		},
		{
			// A bill of 205.00 with 100.00 already paid by bank transfer,
			// a payment of its own without the addon.
			name: "rest of a bill in cash",
			change: func(pmt *bill.Payment) {
				l := paymentLine("105.00", false)
				l.Document = &org.DocumentRef{Series: "FV", Code: "2027-0043"}
				l.Payable = num.NewAmount(20500, 2)
				l.Advances = num.NewAmount(10000, 2)
				pmt.Lines = []*bill.PaymentLine{l}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "105.00", data.Total)
			},
		},
		{
			// A bill of 99.60 rounded up to 100.00 in cash: the line states the
			// money received and leaves the bill's payable out.
			name: "cash rounding",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("100.00", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "100.00", data.Total)
			},
		},
		{
			// Meal vouchers are paid like money, not drawn from prepaid
			// credit (item description, item 12, example 2).
			name: "meal vouchers and card",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("550.00", false)}
				pmt.Methods = []*pay.Record{
					paidWith(pay.MeansKeyOther, "100.00"),
					paidWith(pay.MeansKeyCard, "450.00"),
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "550.00", data.Total)
				assert.Empty(t, data.PrepaidDrawn)
			},
		},
		{
			name: "cash and card",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("550.00", false)}
				pmt.Methods = []*pay.Record{
					paidWith(pay.MeansKeyCash, "100.00"),
					paidWith(pay.MeansKeyCard, "450.00"),
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "550.00", data.Total)
			},
		},
		{
			// An hour at the gym paid from the credit loaded earlier
			// (item description, item 14, example 1).
			name: "drawn from prepaid credit",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("290.00", false)}
				pmt.Methods = []*pay.Record{drawn("")}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "290.00", data.Total)
				assert.Equal(t, "290.00", data.PrepaidDrawn)
			},
		},
		{
			name: "partly drawn from prepaid credit",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("290.00", false)}
				pmt.Methods = []*pay.Record{
					drawn("200.00"),
					paidWith(pay.MeansKeyCash, "90.00"),
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "290.00", data.Total)
				assert.Equal(t, "200.00", data.PrepaidDrawn)
			},
		},
		{
			// The refund goes back to the credit it was drawn from (item
			// description, item 14, example 3).
			name: "refund to prepaid credit",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{paymentLine("121.00", true)}
				pmt.Methods = []*pay.Record{drawn("")}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "-121.00", data.Total)
				assert.Equal(t, "-121.00", data.PrepaidDrawn)
			},
		},
		{
			// Nothing drawn in the end: items that don't apply are left out,
			// not sent as zero (item description, common information).
			name: "prepaid credit drawn and put back",
			change: func(pmt *bill.Payment) {
				pmt.Methods = []*pay.Record{
					drawn("50.00"),
					drawn("-50.00"),
					paidWith(pay.MeansKeyCash, "205.00"),
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "205.00", data.Total)
				assert.Empty(t, data.PrepaidDrawn)
			},
		},
		{
			// A wallet topped up at the gym (item description, item 13).
			name: "credit loaded",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{loadLine("1000.00", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "1000.00", data.Total)
				assert.Equal(t, "1000.00", data.PrepaidLoaded)
				assert.Empty(t, data.PrepaidDrawn)
			},
		},
		{
			// A wallet topped up at the gym and a drink paid in the same
			// payment (item description, item 14, example 1).
			name: "credit loaded with a purchase",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{loadLine("1000.00", false), paymentLine("50.00", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "1050.00", data.Total)
				assert.Equal(t, "1000.00", data.PrepaidLoaded)
				assert.Empty(t, data.PrepaidDrawn)
			},
		},
		{
			// A line marked "false" doesn't load credit.
			name: "line marked as no load",
			change: func(pmt *bill.Payment) {
				line := paymentLine("50.00", false)
				line.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyPrepaidLoad: addon.ExtCodeFalse})
				pmt.Lines = []*bill.PaymentLine{loadLine("1000.00", false), line}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "1050.00", data.Total)
				assert.Equal(t, "1000.00", data.PrepaidLoaded)
			},
		},
		{
			// Credit paid back in cash is a refund of the load, recorded
			// negative as every refund is (item description, chapter 4).
			name: "load paid back",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{loadLine("1000.00", true)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "-1000.00", data.Total)
				assert.Equal(t, "-1000.00", data.PrepaidLoaded)
			},
		},
		{
			// A load and its refund that cancel out: the item is left out,
			// not sent as zero.
			name: "load that cancels out",
			change: func(pmt *bill.Payment) {
				pmt.Lines = []*bill.PaymentLine{loadLine("50.00", false), loadLine("50.00", true)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "0.00", data.Total)
				assert.Empty(t, data.PrepaidLoaded)
			},
		},
		{
			name: "drawn from prepaid credit in EUR",
			change: func(pmt *bill.Payment) {
				inEUR(pmt)
				pmt.Lines = []*bill.PaymentLine{paymentLine("12.33", false)}
				pmt.Methods = []*pay.Record{
					drawn("10.00"),
					paidWith(pay.MeansKeyCash, "2.33"),
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "309.79", data.Total)
				assert.Equal(t, "251.25", data.PrepaidDrawn)
			},
		},
		{
			name: "in EUR, converted to CZK",
			change: func(pmt *bill.Payment) {
				inEUR(pmt)
				pmt.Lines = []*bill.PaymentLine{paymentLine("10.00", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				// 10.00 EUR at 25.125 is 251.25 CZK.
				assert.Equal(t, "251.25", data.Total)
			},
		},
		{
			name: "in EUR, rounded to cents in CZK",
			change: func(pmt *bill.Payment) {
				inEUR(pmt)
				pmt.Lines = []*bill.PaymentLine{paymentLine("12.33", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				// 12.33 EUR at 25.125 is 309.79125 CZK.
				assert.Equal(t, "309.79", data.Total)
			},
		},
		{
			name: "in JPY, which has no decimals, converted to CZK",
			change: func(pmt *bill.Payment) {
				pmt.Currency = currency.JPY
				pmt.ExchangeRates = []*currency.ExchangeRate{
					{From: currency.JPY, To: currency.CZK, Amount: num.MakeAmount(1512, 4)},
				}
				pmt.Lines = []*bill.PaymentLine{paymentLine("1003", false)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				// 1003 JPY at 0.1512 is 151.6536 CZK.
				assert.Equal(t, "151.65", data.Total)
			},
		},
		{
			// 1 JPY at 0.004 is under half a haléř: zero, never "-0.00".
			name: "refund that rounds to nothing in CZK",
			change: func(pmt *bill.Payment) {
				pmt.Currency = currency.JPY
				pmt.ExchangeRates = []*currency.ExchangeRate{
					{From: currency.JPY, To: currency.CZK, Amount: num.MakeAmount(40, 4)},
				}
				pmt.Lines = []*bill.PaymentLine{paymentLine("1", true)}
				paidInCash(pmt)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "0.00", data.Total)
			},
		},
		{
			name: "with a customer",
			change: func(pmt *bill.Payment) {
				pmt.Customer = &org.Party{
					Name:  "Zámečnictví Brumlík s.r.o.",
					TaxID: &tax.Identity{Country: "CZ", Code: "31415920"},
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, &Data{
					TaxID:    "CZ25596641",
					UnitID:   "303",
					DeviceID: "POS-1",
					Number:   "PRG/000135",
					SoldAt:   "2027-01-09T16:45:36+01:00",
					Total:    "205.00",
				}, data)
			},
		},
		{
			// The payee receives the money and isn't in the message.
			name: "payee",
			change: func(pmt *bill.Payment) {
				pmt.Payee = &org.Party{
					Name:  "Pokladní služby s.r.o.",
					TaxID: &tax.Identity{Country: "CZ", Code: "31415920"},
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "CZ25596641", data.TaxID)
				assert.Empty(t, data.DelegatingTaxID)
			},
		},
		{
			// Situation 4: the ordering issuer records the sale for the
			// supplier.
			name: "delegated by one taxpayer",
			change: func(pmt *bill.Payment) {
				pmt.Ordering = &bill.Ordering{Issuer: &org.Party{
					Name:  "Čerpací stanice Brumlík s.r.o.",
					TaxID: &tax.Identity{Country: "CZ", Code: "36055514"},
				}}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "CZ36055514", data.TaxID)
				assert.Equal(t, "CZ25596641", data.DelegatingTaxID)
				assert.False(t, data.DelegatedByMany)
			},
		},
		{
			// Situation 5, example 2: a third party records the sale of
			// several taxpayers, naming one of them.
			name: "delegated by many taxpayers to a third party",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyMultipleTaxpayers, addon.ExtCodeTrue)
				pmt.Ordering = &bill.Ordering{Issuer: &org.Party{
					Name:  "Parkuj v klidu s.r.o.",
					TaxID: &tax.Identity{Country: "CZ", Code: "16180330"},
				}}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "CZ16180330", data.TaxID)
				assert.Equal(t, "CZ25596641", data.DelegatingTaxID)
				assert.True(t, data.DelegatedByMany)
			},
		},
		{
			// Situation 5, example 1: one of the taxpayers records it.
			name: "delegated by many taxpayers to one of them",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyMultipleTaxpayers, addon.ExtCodeTrue)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "CZ25596641", data.TaxID)
				assert.Empty(t, data.DelegatingTaxID)
				assert.True(t, data.DelegatedByMany)
			},
		},
		{
			name: "self-service kiosk",
			change: func(pmt *bill.Payment) {
				pmt.Ext = pmt.Ext.Set(addon.ExtKeyDevice, "KIOSK-3")
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "KIOSK-3", data.DeviceID)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pmt := loadExample(t, "receipt")
			if tt.change != nil {
				tt.change(pmt)
				require.NoError(t, pmt.Calculate())
			}
			require.NoError(t, rules.Validate(pmt))
			sale, err := testSale(pmt, testOptions, testNow)
			require.NoError(t, err)
			tt.check(t, sale.Data)
			validateSchema(t, saleXML(t, sale))
		})
	}
}

func TestNewSaleHeader(t *testing.T) {
	pmt := loadExample(t, "receipt")

	t.Run("first attempt", func(t *testing.T) {
		sale, err := newSale(pmt, testOptions)
		require.NoError(t, err)
		assert.Equal(t, &Header{
			UUID:         "8c1a7ad4-5f3e-4b2a-9d61-0e7f3c2b1a90",
			FirstAttempt: true,
		}, sale.Header, "the send time is Sign's to set")
		assert.Equal(t, "2027-01-09T16:45:37+01:00", sentAt(time.Date(2027, 1, 9, 15, 45, 37, 987654321, time.UTC)))
	})
	t.Run("resend", func(t *testing.T) {
		sale, err := new(Client).saleFor(pmt, WithResend())
		require.NoError(t, err)
		assert.False(t, sale.Header.FirstAttempt)
		assert.Nil(t, sale.Header.Verification)
		validateSchema(t, saleXML(t, sale))
	})
	t.Run("verification", func(t *testing.T) {
		sale, err := new(Client).saleFor(pmt, WithVerification())
		require.NoError(t, err)
		assert.True(t, sale.Header.FirstAttempt)
		require.NotNil(t, sale.Header.Verification)
		assert.True(t, *sale.Header.Verification)
		validateSchema(t, saleXML(t, sale))
	})
	t.Run("new UUID v4 and send time for every message", func(t *testing.T) {
		first, err := new(Client).saleFor(pmt)
		require.NoError(t, err)
		second, err := new(Client).saleFor(pmt)
		require.NoError(t, err)
		v4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
		assert.Regexp(t, v4, first.Header.UUID)
		assert.Regexp(t, v4, second.Header.UUID)
		assert.NotEqual(t, first.Header.UUID, second.Header.UUID)
		assert.WithinDuration(t, time.Now(), mustParseTime(t, first.Header.SentAt), time.Minute)
		assert.Equal(t, first.Data, second.Data)
	})
	t.Run("resends keep the sale key", func(t *testing.T) {
		first, err := new(Client).saleFor(pmt, WithVerification())
		require.NoError(t, err)
		second, err := new(Client).saleFor(pmt, WithResend(), WithVerification())
		require.NoError(t, err)
		assert.False(t, second.Header.FirstAttempt)
		assert.NotEqual(t, first.Header.UUID, second.Header.UUID)
		assert.Equal(t, first.Data.SaleKey(), second.Data.SaleKey())
	})
}

// A payment the addon's or GOBL's rules refuse builds no sale.
func TestNewSaleErrors(t *testing.T) {
	tests := []struct {
		name   string
		change func(pmt *bill.Payment)
		err    string
	}{
		{
			name:   "without the addon",
			change: func(pmt *bill.Payment) { pmt.Addons = tax.WithAddons() },
			err:    "validation: missing 'cz-eet-v1' addon",
		},
		{
			name:   "total of 0.00 against lines of 205.00",
			change: func(pmt *bill.Payment) { pmt.Total = amount("0.00") },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-12] payment total must be the sum of its lines",
		},
		{
			name: "total and method of 1.00 against lines of 205.00",
			change: func(pmt *bill.Payment) {
				pmt.Total = amount("1.00")
				pmt.Methods[0].Amount = amount("1.00")
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-12] payment total must be the sum of its lines",
		},
		{
			name:   "no code",
			change: func(pmt *bill.Payment) { pmt.Code = "" },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-03] ($.code) payment code is required",
		},
		{
			name:   "request",
			change: func(pmt *bill.Payment) { pmt.Type = bill.PaymentTypeRequest },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-01] ($.type) payment type must be receipt",
		},
		{
			name:   "no supplier",
			change: func(pmt *bill.Payment) { pmt.Supplier = nil },
			err:    "payment is not valid:",
		},
		{
			name: "German supplier",
			change: func(pmt *bill.Payment) {
				pmt.Supplier.TaxID = &tax.Identity{Country: "DE", Code: "111111125"}
			},
			err: "[GOBL-CZ-EET-BILL-PAYMENT-07] ($.supplier.tax_id.country) supplier tax ID must be Czech",
		},
		{
			name:   "no issue time",
			change: func(pmt *bill.Payment) { pmt.IssueTime = nil },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-05] ($.issue_time) issue time is required",
		},
		{
			name:   "no unit",
			change: func(pmt *bill.Payment) { pmt.Ext = pmt.Ext.Delete(addon.ExtKeyUnit) },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-08] ($.ext) extensions 'cz-eet-unit' and 'cz-eet-device' are required",
		},
		{
			name:   "no exchange rate",
			change: func(pmt *bill.Payment) { pmt.Currency = currency.EUR },
			err:    "[GOBL-CZ-EET-BILL-PAYMENT-02] payment must be in CZK or provide an exchange rate to CZK",
		},
		{
			name: "prepaid credit in a currency without a rate",
			change: func(pmt *bill.Payment) {
				m := drawn("10.00")
				m.Currency = currency.EUR
				pmt.Methods = []*pay.Record{m, paidWith(pay.MeansKeyCash, "195.00")}
			},
			err: "[GOBL-BILL-PAYMENT-08] methods sum must match total",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pmt := loadExample(t, "receipt")
			tt.change(pmt)
			_, err := new(Client).saleFor(pmt)
			assert.ErrorIs(t, err, ErrValidation)
			assert.ErrorContains(t, err, tt.err)
		})
	}
	t.Run("faults reachable", func(t *testing.T) {
		pmt := loadExample(t, "receipt")
		pmt.Methods = []*pay.Record{paidWith(pay.MeansKeyCash, "200.00")}
		_, err := new(Client).saleFor(pmt)
		assert.ErrorIs(t, err, ErrValidation)
		var faults rules.Faults
		require.ErrorAs(t, err, &faults)
		assert.True(t, faults.HasCode("GOBL-BILL-PAYMENT-08"))
	})
	t.Run("no payment", func(t *testing.T) {
		_, err := new(Client).saleFor(nil)
		assert.EqualError(t, err, "validation: no payment")
	})
}

// The example of the interface description, chapter 4.
func TestSaleKey(t *testing.T) {
	data := &Data{
		TaxID:    "CZ00000019",
		UnitID:   "243",
		DeviceID: "24/A-6/Brno_2",
		Number:   "#135433c/11/2027",
		SoldAt:   "2027-01-09T16:45:36+01:00",
		Total:    "3264.50",
	}
	assert.Equal(t, "CZ00000019|243|24/A-6/Brno_2|#135433c/11/2027|2027-01-09T16:45:36+01:00|3264.50", data.SaleKey())
}

// loadExample reads the calculated payment of an example's golden output.
func loadExample(t *testing.T, name string) *bill.Payment {
	t.Helper()
	pmt, ok := loadExampleEnvelope(t, name).Extract().(*bill.Payment)
	require.True(t, ok)
	return pmt
}

func loadExampleEnvelope(t *testing.T, name string) *gobl.Envelope {
	t.Helper()
	data, err := os.ReadFile("examples/out/" + name + ".json")
	require.NoError(t, err)
	env := new(gobl.Envelope)
	require.NoError(t, json.Unmarshal(data, env))
	return env
}

// saleXML is the sale as a standalone, indented document with its
// namespace declared, as it would sit in the SOAP body.
func saleXML(t *testing.T, sale *Sale) []byte {
	t.Helper()
	data, err := xml.Marshal(sale)
	require.NoError(t, err)
	doc := etree.NewDocument()
	require.NoError(t, doc.ReadFromBytes(data))
	doc.Root().CreateAttr("xmlns:v4", nsEET)
	doc.Indent(2)
	out, err := doc.WriteToBytes()
	require.NoError(t, err)
	return out
}

// Values at the edges of what EET takes for the sale number, the unit and
// the device go through the addon's rules and the schema alike: what the
// rules accept the schema accepts, and what they refuse it refuses.
func TestEdgeValues(t *testing.T) {
	number := func(series, code string) func(*bill.Payment) {
		return func(pmt *bill.Payment) { pmt.Series, pmt.Code = cbc.Code(series), cbc.Code(code) }
	}
	ext := func(key cbc.Key, value string) func(*bill.Payment) {
		return func(pmt *bill.Payment) { pmt.Ext = pmt.Ext.Set(key, cbc.Code(value)) }
	}
	tests := []struct {
		name   string
		change func(pmt *bill.Payment)
		// fault is the rule that refuses the value, and message puts the
		// same value straight into the sale for the schema to refuse.
		fault   string
		message func(data *Data)
	}{
		{name: "number of 25 characters", change: number("ABCDEFGHIJKLMNOPQRST", "1234")},
		{
			name:    "number of 26 characters",
			change:  number("ABCDEFGHIJKLMNOPQRST", "12345"),
			fault:   "[GOBL-CZ-EET-BILL-PAYMENT-04]",
			message: func(data *Data) { data.Number = "ABCDEFGHIJKLMNOPQRST/12345" },
		},
		{name: "number with every symbol", change: number("A.B,C:D;E", "F#G-H_I 1")},
		{name: "number of the interface description", change: number("#135433c/11", "2027")},
		{
			name:    "number with a symbol EET doesn't take",
			change:  number("PRG", "0001+1"),
			fault:   "[GOBL-CZ-EET-BILL-PAYMENT-04]",
			message: func(data *Data) { data.Number = "PRG/0001+1" },
		},
		{name: "unit 1", change: ext(addon.ExtKeyUnit, "1")},
		{name: "unit of the interface description", change: ext(addon.ExtKeyUnit, "243")},
		{name: "unit 999999999", change: ext(addon.ExtKeyUnit, "999999999")},
		{
			name:    "unit 1000000000",
			change:  ext(addon.ExtKeyUnit, "1000000000"),
			fault:   "[GOBL-CZ-EET-BILL-PAYMENT-09]",
			message: func(data *Data) { data.UnitID = "1000000000" },
		},
		{
			name:    "unit 0",
			change:  ext(addon.ExtKeyUnit, "0"),
			fault:   "[GOBL-CZ-EET-BILL-PAYMENT-09]",
			message: func(data *Data) { data.UnitID = "0" },
		},
		{name: "device of 20 characters", change: ext(addon.ExtKeyDevice, "ABCDEFGHIJ0123456789")},
		{
			name:    "device of 21 characters",
			change:  ext(addon.ExtKeyDevice, "ABCDEFGHIJ0123456789X"),
			fault:   "[GOBL-CZ-EET-BILL-PAYMENT-10]",
			message: func(data *Data) { data.DeviceID = "ABCDEFGHIJ0123456789X" },
		},
		{name: "device with every symbol", change: ext(addon.ExtKeyDevice, "a.b,c:d;e/f#g-h_i 1")},
		{name: "device of the interface description", change: ext(addon.ExtKeyDevice, "24/A-6/Brno_2")},
		{
			name:    "device with a symbol EET doesn't take",
			change:  ext(addon.ExtKeyDevice, "POS+1"),
			fault:   "[GOBL-CZ-EET-BILL-PAYMENT-10]",
			message: func(data *Data) { data.DeviceID = "POS+1" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pmt := loadExample(t, "receipt")
			tt.change(pmt)
			require.NoError(t, pmt.Calculate())
			err := rules.Validate(pmt)
			if tt.fault == "" {
				require.NoError(t, err)
				sale, err := testSale(pmt, testOptions, testNow)
				require.NoError(t, err)
				validateSchema(t, saleXML(t, sale))
				return
			}
			assert.ErrorContains(t, err, tt.fault)
			sale, serr := testSale(loadExample(t, "receipt"), testOptions, testNow)
			require.NoError(t, serr)
			tt.message(sale.Data)
			assert.Error(t, schemaError(t, saleXML(t, sale)))
		})
	}
}

// The README shows the receipt example, without the UUID that keeps the
// example's golden files stable.
func TestReadmeExample(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	_, rest, found := strings.Cut(string(readme), "```yaml\n")
	require.True(t, found)
	shown, _, found := strings.Cut(rest, "```")
	require.True(t, found)

	example, err := os.ReadFile("examples/receipt.yaml")
	require.NoError(t, err)
	var lines []string
	for line := range strings.SplitSeq(string(example), "\n") {
		if !strings.HasPrefix(line, "uuid: ") {
			lines = append(lines, line)
		}
	}
	assert.Equal(t, strings.Join(lines, "\n"), shown)
}

// The schema check catches the values the Playground answers with error 3.
func TestSchemaRejects(t *testing.T) {
	tests := map[string]func(data *Data){
		"leading zero in the total": func(data *Data) { data.Total = "020.45" },
		"negative zero total":       func(data *Data) { data.Total = "-0.00" },
		"one decimal":               func(data *Data) { data.Total = "205.0" },
		"fractional seconds":        func(data *Data) { data.SoldAt = "2027-01-09T16:45:36.5+01:00" },
		"no zone":                   func(data *Data) { data.SoldAt = "2027-01-09T16:45:36" },
		"unit with a leading zero":  func(data *Data) { data.UnitID = "0303" },
		"accented device":           func(data *Data) { data.DeviceID = "POKLADNA-Č1" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			sale, err := testSale(loadExample(t, "receipt"), testOptions, testNow)
			require.NoError(t, err)
			change(sale.Data)
			assert.Error(t, schemaError(t, saleXML(t, sale)))
		})
	}
}

func validateSchema(t *testing.T, data []byte) {
	t.Helper()
	if err := schemaError(t, data); err != nil {
		t.Fatalf("sale does not validate against the schema: %v\n%s", err, data)
	}
}

func schemaError(t *testing.T, data []byte) error {
	t.Helper()
	ctx := context.Background()
	compiling := helium.NewErrorCollector(ctx, helium.ErrorLevelNone)
	schema, err := xsd.NewCompiler().
		FS(helium.PermissiveFS()).
		ErrorHandler(compiling).
		CompileFile(ctx, "schemas/EETXMLSchema.xsd")
	require.NoError(t, errors.Join(append(compiling.Errors(), err)...))
	doc, err := helium.NewParser().Parse(ctx, data)
	require.NoError(t, err)
	validating := helium.NewErrorCollector(ctx, helium.ErrorLevelNone)
	if err := xsd.NewValidator(schema).ErrorHandler(validating).Validate(ctx, doc); err != nil {
		return errors.Join(append(validating.Errors(), err)...)
	}
	return nil
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(timeFormat, value)
	require.NoError(t, err)
	return parsed
}
