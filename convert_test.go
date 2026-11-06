package eet

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/invopop/gobl"
	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
	"github.com/lestrrat-go/libxml2"
	"github.com/lestrrat-go/libxml2/xsd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testOptions fix the message UUID and send time, which NewSale takes from
// a random UUID and the clock.
var testOptions = options{
	uuid: "8c1a7ad4-5f3e-4b2a-9d61-0e7f3c2b1a90",
	now:  time.Date(2027, 1, 9, 15, 45, 37, 0, time.UTC),
}

func TestNewSaleExamples(t *testing.T) {
	for _, name := range []string{"invoice", "credit-note"} {
		t.Run(name, func(t *testing.T) {
			sale, err := newSale(loadExample(t, name), testOptions)
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

func TestNewSaleData(t *testing.T) {
	tests := []struct {
		name   string
		change func(inv *bill.Invoice)
		check  func(t *testing.T, data *Data)
	}{
		{
			name: "sale",
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, &Data{
					TaxID:    "CZ12345679",
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
			change: func(inv *bill.Invoice) {
				inv.IssueDate = cal.MakeDate(2027, 7, 1)
				inv.IssueTime = cal.NewTime(12, 0, 5)
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "2027-07-01T12:00:05+02:00", data.SoldAt)
			},
		},
		{
			name:   "no series",
			change: func(inv *bill.Invoice) { inv.Series = "" },
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "000135", data.Number)
			},
		},
		{
			name: "refund",
			change: func(inv *bill.Invoice) {
				inv.Type = bill.InvoiceTypeCreditNote
				inv.Preceding = []*org.DocumentRef{{Series: "PRG", Code: "000134"}}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "-205.00", data.Total)
			},
		},
		{
			name: "refund of nothing",
			change: func(inv *bill.Invoice) {
				inv.Type = bill.InvoiceTypeCreditNote
				inv.Preceding = []*org.DocumentRef{{Series: "PRG", Code: "000134"}}
				for _, line := range inv.Lines {
					line.Item.Price = num.NewAmount(0, 2)
				}
			},
			check: func(t *testing.T, data *Data) {
				assert.Equal(t, "0.00", data.Total)
			},
		},
		{
			name: "in EUR, converted to CZK",
			change: func(inv *bill.Invoice) {
				inv.Currency = currency.EUR
				inv.ExchangeRates = []*currency.ExchangeRate{
					{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(25125, 3)},
				}
				inv.Lines = inv.Lines[:1]
				inv.Lines[0].Quantity = num.MakeAmount(1, 0)
				inv.Lines[0].Item.Price = num.NewAmount(1233, 2)
			},
			check: func(t *testing.T, data *Data) {
				// 12.33 EUR at 25.125 is 309.79125 CZK.
				assert.Equal(t, "309.79", data.Total)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := loadExample(t, "invoice")
			if tt.change != nil {
				tt.change(inv)
				require.NoError(t, inv.Calculate())
			}
			sale, err := newSale(inv, testOptions)
			require.NoError(t, err)
			tt.check(t, sale.Data)
			validateSchema(t, saleXML(t, sale))
		})
	}
}

func TestNewSaleHeader(t *testing.T) {
	inv := loadExample(t, "invoice")

	t.Run("first attempt", func(t *testing.T) {
		o := testOptions
		o.now = time.Date(2027, 1, 9, 15, 45, 37, 987654321, time.UTC)
		sale, err := newSale(inv, o)
		require.NoError(t, err)
		assert.Equal(t, &Header{
			UUID:         "8c1a7ad4-5f3e-4b2a-9d61-0e7f3c2b1a90",
			SentAt:       "2027-01-09T16:45:37+01:00",
			FirstAttempt: true,
		}, sale.Header)
	})
	t.Run("resend", func(t *testing.T) {
		sale, err := NewSale(inv, WithResend())
		require.NoError(t, err)
		assert.False(t, sale.Header.FirstAttempt)
		assert.Nil(t, sale.Header.Verification)
		validateSchema(t, saleXML(t, sale))
	})
	t.Run("verification", func(t *testing.T) {
		sale, err := NewSale(inv, WithVerification())
		require.NoError(t, err)
		assert.True(t, sale.Header.FirstAttempt)
		require.NotNil(t, sale.Header.Verification)
		assert.True(t, *sale.Header.Verification)
		validateSchema(t, saleXML(t, sale))
	})
	t.Run("new UUID v4 and send time for every message", func(t *testing.T) {
		first, err := NewSale(inv)
		require.NoError(t, err)
		second, err := NewSale(inv)
		require.NoError(t, err)
		v4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
		assert.Regexp(t, v4, first.Header.UUID)
		assert.Regexp(t, v4, second.Header.UUID)
		assert.NotEqual(t, first.Header.UUID, second.Header.UUID)
		assert.WithinDuration(t, time.Now(), mustParseTime(t, first.Header.SentAt), time.Minute)
		assert.Equal(t, first.Data, second.Data)
	})
}

func TestNewSaleErrors(t *testing.T) {
	tests := []struct {
		name   string
		change func(inv *bill.Invoice)
		err    string
	}{
		{
			name:   "without the addon",
			change: func(inv *bill.Invoice) { inv.Addons = tax.WithAddons() },
			err:    "eet: invoice must use the cz-eet-v1 addon",
		},
		{
			name:   "not calculated",
			change: func(inv *bill.Invoice) { inv.Totals = nil },
			err:    "eet: invoice must be calculated",
		},
		{
			name:   "proforma",
			change: func(inv *bill.Invoice) { inv.Type = bill.InvoiceTypeProforma },
			err:    "eet: invoice type must be standard or credit-note",
		},
		{
			name:   "no supplier tax ID",
			change: func(inv *bill.Invoice) { inv.Supplier.TaxID = nil },
			err:    "eet: invoice needs the supplier's tax ID",
		},
		{
			name: "German supplier",
			change: func(inv *bill.Invoice) {
				inv.Supplier.TaxID = &tax.Identity{Country: "DE", Code: "111111125"}
			},
			err: "eet: supplier tax ID must be Czech",
		},
		{
			name:   "no issue time",
			change: func(inv *bill.Invoice) { inv.IssueTime = nil },
			err:    "eet: invoice needs an issue time",
		},
		{
			name:   "no device",
			change: func(inv *bill.Invoice) { inv.Tax.Ext = inv.Tax.Ext.Delete(addon.ExtKeyDevice) },
			err:    "eet: invoice needs the cz-eet-unit and cz-eet-device extensions",
		},
		{
			name:   "no exchange rate",
			change: func(inv *bill.Invoice) { inv.Currency = currency.EUR },
			err:    "eet: invoice needs an exchange rate to CZK",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := loadExample(t, "invoice")
			tt.change(inv)
			_, err := NewSale(inv)
			assert.EqualError(t, err, tt.err)
		})
	}
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

// loadExample reads the calculated invoice of an example's golden output.
func loadExample(t *testing.T, name string) *bill.Invoice {
	t.Helper()
	data, err := os.ReadFile("examples/out/" + name + ".json")
	require.NoError(t, err)
	env := new(gobl.Envelope)
	require.NoError(t, json.Unmarshal(data, env))
	inv, ok := env.Extract().(*bill.Invoice)
	require.True(t, ok)
	return inv
}

// saleXML is the sale as a standalone, indented document with its
// namespace declared, as it would sit in the SOAP body.
func saleXML(t *testing.T, sale *Sale) []byte {
	t.Helper()
	data, err := xml.Marshal(sale)
	require.NoError(t, err)
	doc := etree.NewDocument()
	require.NoError(t, doc.ReadFromBytes(data))
	doc.Root().CreateAttr("xmlns:v4", NamespaceEET)
	doc.Indent(2)
	out, err := doc.WriteToBytes()
	require.NoError(t, err)
	return out
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
			sale, err := newSale(loadExample(t, "invoice"), testOptions)
			require.NoError(t, err)
			change(sale.Data)
			assert.Error(t, schemaError(t, saleXML(t, sale)))
		})
	}
}

func validateSchema(t *testing.T, data []byte) {
	t.Helper()
	if err := schemaError(t, data); err != nil {
		var details []error
		if verr, ok := err.(xsd.SchemaValidationError); ok {
			details = verr.Errors()
		}
		t.Fatalf("sale does not validate against the schema: %v %v\n%s", err, details, data)
	}
}

func schemaError(t *testing.T, data []byte) error {
	t.Helper()
	schema, err := xsd.ParseFromFile("schemas/EETXMLSchema.xsd")
	require.NoError(t, err)
	defer schema.Free()
	doc, err := libxml2.Parse(data)
	require.NoError(t, err)
	defer doc.Free()
	return schema.Validate(doc)
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(timeFormat, value)
	require.NoError(t, err)
	return parsed
}
