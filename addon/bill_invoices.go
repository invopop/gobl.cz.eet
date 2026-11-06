package addon

import (
	"regexp"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
	"github.com/invopop/gobl/tax"
)

// SaleNumber joins the invoice series and code the way EET receives them
// as the sale's sequence number (porad_cis): "SERIES/CODE", or the code
// alone without a series.
func SaleNumber(inv *bill.Invoice) string {
	if inv.Series == "" {
		return inv.Code.String()
	}
	return inv.Series.String() + "/" + inv.Code.String()
}

// saleNumberPattern is the porad_cis mask of the EET schema. GOBL codes
// accept more characters than this, accented letters among them.
var saleNumberPattern = regexp.MustCompile(`^[0-9a-zA-Z\.,:;/#\-_ ]{1,25}$`)

// maxTotal is the largest amount, positive or negative, that EET takes.
var maxTotal = num.MakeAmount(9999999999, 2)

// billInvoiceRules covers what the EET message needs and an invoice may
// lack: only sales types, a Czech merchant, the unit and device the sale
// happened on, a sale time, and a number and total EET can take.
func billInvoiceRules() *rules.Set {
	return rules.For(new(bill.Invoice),
		rules.Field("type",
			rules.Assert("01", "invoice type must be standard or credit-note, the sales and refunds EET records",
				is.In(bill.InvoiceTypeStandard, bill.InvoiceTypeCreditNote),
			),
		),
		rules.Assert("02", "invoice must be in CZK or provide an exchange rate to CZK (EET celk_trzba)",
			currency.CanConvertTo(currency.CZK),
		),
		rules.Field("code",
			rules.Assert("03", "invoice code is required (EET porad_cis)", is.Present),
		),
		rules.Assert("04", "invoice series and code joined with '/' must be at most 25 letters, digits, spaces or . , : ; / # - _ (EET porad_cis)",
			is.Func("sale number fits", saleNumberFits),
		),
		rules.Field("issue_time",
			rules.Assert("05", "issue time is required (EET dat_trzby)", is.Present),
		),
		rules.Field("supplier",
			rules.Field("tax_id",
				rules.Assert("06", "supplier tax ID with a code is required (EET eic_popl)",
					is.Func("tax ID with a code", taxIDHasCode),
				),
				rules.Field("country",
					rules.Assert("07", "supplier tax ID must be Czech (EET eic_popl)", is.In(l10n.TaxCountryCode(l10n.CZ))),
				),
			),
		),
		rules.Field("tax",
			rules.Field("ext",
				rules.Assert("08", "tax extensions 'cz-eet-unit' and 'cz-eet-device' are required",
					tax.ExtensionsRequire(ExtKeyUnit, ExtKeyDevice),
				),
				rules.Assert("09", "tax extension 'cz-eet-unit' must be a number from 1 to 999999999 (EET id_jednotky)",
					tax.ExtensionHasValidCode(ExtKeyUnit),
				),
				rules.Assert("10", "tax extension 'cz-eet-device' must be 1 to 20 letters, digits, spaces or . , : ; / # - _ (EET id_pokl)",
					tax.ExtensionHasValidCode(ExtKeyDevice),
				),
			),
		),
		rules.Assert("11", "payable total must be at most 99,999,999.99 CZK (EET celk_trzba)",
			is.Func("total fits", totalFits),
		),
	)
}

// normalizeInvoice prepares the tax object that holds the unit and device
// extensions, so a missing one is reported as missing extensions.
func normalizeInvoice(inv *bill.Invoice) {
	if inv.Tax == nil {
		inv.Tax = new(bill.Tax)
	}
}

func taxIDHasCode(val any) bool {
	id, ok := val.(*tax.Identity)
	return ok && id != nil && id.Code != ""
}

// saleNumberFits leaves a missing code to rule 03.
func saleNumberFits(val any) bool {
	inv, ok := val.(*bill.Invoice)
	if !ok || inv.Code == "" {
		return true
	}
	return saleNumberPattern.MatchString(SaleNumber(inv))
}

// totalFits leaves missing totals and exchange rates to core and rule 02.
func totalFits(val any) bool {
	inv, ok := val.(*bill.Invoice)
	if !ok || inv.Totals == nil {
		return true
	}
	total := currency.Convert(inv.ExchangeRates, inv.Currency, currency.CZK, inv.Totals.Payable)
	if total == nil {
		return true
	}
	return total.Abs().Rescale(2).Compare(maxTotal) <= 0
}
