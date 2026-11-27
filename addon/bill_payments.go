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

// SaleNumber joins the payment series and code the way EET receives them
// as the sale's sequence number (porad_cis): "SERIES/CODE", or the code
// alone without a series.
func SaleNumber(pmt *bill.Payment) string {
	if pmt == nil {
		return ""
	}
	if pmt.Series == "" {
		return pmt.Code.String()
	}
	return pmt.Series.String() + "/" + pmt.Code.String()
}

// SaleTotal is the payment's total the way EET receives it as the sale
// total (celk_trzba): in CZK with two decimals, or nil without an exchange
// rate to CZK.
func SaleTotal(pmt *bill.Payment) *num.Amount {
	if pmt == nil {
		return nil
	}
	return toCZK(pmt, pmt.Total)
}

// PrepaidDrawn is the sum of the methods whose key has MeansKeyPrepaid, the way EET receives it (cerp_zuct): in CZK with two decimals,
// or nil when no method is marked or an exchange rate is missing.
func PrepaidDrawn(pmt *bill.Payment) *num.Amount {
	if pmt == nil {
		return nil
	}
	// Added up as GOBL checks the methods against the total: from the
	// currency's decimals, as Add keeps those of the first amount.
	exp := pmt.Total.Exp()
	if def := pmt.Currency.Def(); def != nil {
		exp = def.Zero().Exp()
	}
	sum := num.MakeAmount(0, exp)
	marked := false
	for _, m := range pmt.Methods {
		if m == nil || !m.Key.Has(MeansKeyPrepaid) {
			continue
		}
		from := m.Currency
		if from == currency.CodeEmpty {
			from = pmt.Currency
		}
		amount := currency.Convert(exchangeRates(pmt), from, pmt.Currency, m.Amount)
		if amount == nil {
			return nil
		}
		sum = sum.Add(*amount)
		marked = true
	}
	if !marked {
		return nil
	}
	return toCZK(pmt, sum)
}

// PrepaidLoaded is the sum of the lines marked with ExtKeyPrepaidLoad,
// refund lines subtracted, the way EET receives it (urceno_cerp_zuct): in CZK
// with two decimals, or nil when no line is marked or there is no exchange
// rate to CZK.
func PrepaidLoaded(pmt *bill.Payment) *num.Amount {
	if pmt == nil {
		return nil
	}
	// Added up with all their decimals, as totalMatchesLines does.
	var exp uint32
	marked := false
	for _, l := range pmt.Lines {
		if loadsCredit(l) {
			exp = max(exp, l.Amount.Exp())
			marked = true
		}
	}
	if !marked {
		return nil
	}
	sum := num.MakeAmount(0, exp)
	for _, l := range pmt.Lines {
		switch {
		case !loadsCredit(l):
		case l.Refund:
			sum = sum.Subtract(l.Amount)
		default:
			sum = sum.Add(l.Amount)
		}
	}
	return toCZK(pmt, sum)
}

func loadsCredit(l *bill.PaymentLine) bool {
	return l != nil && l.Ext.Get(ExtKeyPrepaidLoad) == ExtCodeTrue
}

// MultipleTaxpayers says whether the payment is marked with
// ExtKeyMultipleTaxpayers as income of several taxpayers, the way EET
// receives it (povereni_vice_popl).
func MultipleTaxpayers(pmt *bill.Payment) bool {
	return pmt != nil && pmt.Ext.Get(ExtKeyMultipleTaxpayers) == ExtCodeTrue
}

// toCZK converts an amount in the payment's currency to CZK with two
// decimals, or returns nil without a rate.
func toCZK(pmt *bill.Payment, amount num.Amount) *num.Amount {
	if pmt.Currency != currency.CZK {
		rate := currency.MatchExchangeRate(exchangeRates(pmt), pmt.Currency, currency.CZK)
		if rate == nil {
			return nil
		}
		// conversions keep the precision of the amount, so ensure cents
		if amount.Exp() < 2 {
			amount = amount.Rescale(2)
		}
		amount = rate.Convert(amount)
	}
	amount = amount.Rescale(2)
	return &amount
}

// saleNumberPattern is the porad_cis mask of the EET schema. GOBL codes
// accept more characters than this, accented letters among them.
var saleNumberPattern = regexp.MustCompile(`^[0-9a-zA-Z\.,:;/#\-_ ]{1,25}$`)

// maxAmount and minAmount are the largest and smallest amounts EET takes.
var (
	maxAmount = num.MakeAmount(9999999999, 2)
	minAmount = num.MakeAmount(-9999999999, 2)
)

// billPaymentRules refuses a payment the EET message can't be built from,
// or would misstate.
func billPaymentRules() *rules.Set {
	return rules.For(new(bill.Payment),
		rules.Field("type",
			rules.Assert("01", "payment type must be receipt, the payments EET records",
				is.In(bill.PaymentTypeReceipt),
			),
		),
		rules.Assert("02", "payment must be in CZK or provide an exchange rate to CZK (EET celk_trzba)",
			is.Func("rate to CZK", hasRateToCZK),
		),
		rules.Field("code",
			rules.Assert("03", "payment code is required (EET porad_cis)", is.Present),
		),
		rules.Assert("04", "payment series and code joined with '/' must be at most 25 letters, digits, spaces or . , : ; / # - _ (EET porad_cis)",
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
					rules.Assert("07", "supplier tax ID must be Czech (EET eic_popl)", is.In(l10n.CZ.Tax())),
				),
			),
		),
		rules.Field("ext",
			rules.Assert("08", "extensions 'cz-eet-unit' and 'cz-eet-device' are required",
				tax.ExtensionsRequire(ExtKeyUnit, ExtKeyDevice),
			),
			rules.Assert("09", "extension 'cz-eet-unit' must be a number from 1 to 999999999 (EET id_jednotky)",
				tax.ExtensionHasValidCode(ExtKeyUnit),
			),
			rules.Assert("10", "extension 'cz-eet-device' must be 1 to 20 letters, digits, spaces or . , : ; / # - _ (EET id_pokl)",
				tax.ExtensionHasValidCode(ExtKeyDevice),
			),
			rules.Assert("17", "extension 'cz-eet-multiple-taxpayers' must be 'true' or 'false' (EET povereni_vice_popl)",
				tax.ExtensionHasValidCode(ExtKeyMultipleTaxpayers),
			),
			rules.Assert("18", "extension 'cz-eet-prepaid-load' goes in the payment lines",
				tax.ExtensionsExclude(ExtKeyPrepaidLoad),
			),
		),
		rules.Field("lines",
			rules.Each(
				rules.Field("ext",
					rules.Assert("19", "extension 'cz-eet-prepaid-load' must be 'true' or 'false'",
						tax.ExtensionHasValidCode(ExtKeyPrepaidLoad),
					),
					rules.Assert("20", "extension 'cz-eet-multiple-taxpayers' goes in the payment's extensions",
						tax.ExtensionsExclude(ExtKeyMultipleTaxpayers),
					),
				),
			),
		),
		rules.Assert("11", "payment total must be within ±99,999,999.99 CZK (EET celk_trzba)",
			is.Func("total fits", totalFits),
		),
		rules.Assert("12", "payment total must be the sum of its lines, with refund lines subtracted (EET celk_trzba)",
			is.Func("total matches lines", totalMatchesLines),
		),
		rules.Assert("13", "payment method date must be the payment's issue date, as each payment is recorded on its own (EET dat_trzby)",
			is.Func("methods on the issue date", methodsOnIssueDate),
		),
		rules.Assert("14", "prepaid credit drawn must be within ±99,999,999.99 CZK (EET cerp_zuct)",
			is.Func("prepaid credit drawn fits", prepaidDrawnFits),
		),
		rules.Assert("21", "prepaid credit loaded must be within ±99,999,999.99 CZK (EET urceno_cerp_zuct)",
			is.Func("prepaid credit loaded fits", prepaidLoadedFits),
		),
		rules.Field("ordering",
			rules.Field("issuer",
				rules.Field("tax_id",
					rules.Assert("15", "ordering issuer tax ID with a code is required (EET eic_popl)",
						is.Func("tax ID with a code", taxIDHasCode),
					),
					rules.Field("country",
						rules.Assert("16", "ordering issuer tax ID must be Czech (EET eic_popl)", is.In(l10n.CZ.Tax())),
					),
				),
			),
		),
	)
}

// normalizePayment gives line and method amounts the decimals of their
// currency, as GOBL adds lines up with the decimals of the first one and
// converts a method's amount with the decimals it was written with.
func normalizePayment(pmt *bill.Payment) {
	if pmt == nil {
		return
	}
	cur := pmt.Currency
	if cur == currency.CodeEmpty {
		if r := pmt.RegimeDef(); r != nil {
			cur = r.Currency
		}
	}
	for _, l := range pmt.Lines {
		if l != nil {
			l.Amount = withDecimalsOf(cur, l.Amount)
		}
	}
	for _, m := range pmt.Methods {
		if m == nil {
			continue
		}
		mc := m.Currency
		if mc == currency.CodeEmpty {
			mc = cur
		}
		m.Amount = withDecimalsOf(mc, m.Amount)
	}
}

func withDecimalsOf(cur currency.Code, amount num.Amount) num.Amount {
	def := cur.Def()
	if def == nil {
		return amount
	}
	return amount.RescaleUp(def.Zero().Exp())
}

func hasRateToCZK(val any) bool {
	pmt, ok := val.(*bill.Payment)
	if !ok || pmt.Currency == currency.CZK {
		return true
	}
	return currency.MatchExchangeRate(exchangeRates(pmt), pmt.Currency, currency.CZK) != nil
}

// exchangeRates are the payment's exchange rates, or none when one of them is
// nil, as GOBL's lookups read every entry: a broken list counts as no rate.
func exchangeRates(pmt *bill.Payment) []*currency.ExchangeRate {
	for _, rate := range pmt.ExchangeRates {
		if rate == nil {
			return nil
		}
	}
	return pmt.ExchangeRates
}

func taxIDHasCode(val any) bool {
	id, ok := val.(*tax.Identity)
	return ok && id != nil && id.Code != ""
}

// saleNumberFits leaves a missing code to rule 03.
func saleNumberFits(val any) bool {
	pmt, ok := val.(*bill.Payment)
	if !ok || pmt.Code == "" {
		return true
	}
	return saleNumberPattern.MatchString(SaleNumber(pmt))
}

// totalFits leaves a missing exchange rate to rule 02.
func totalFits(val any) bool {
	pmt, ok := val.(*bill.Payment)
	if !ok {
		return true
	}
	return withinMaxAmount(SaleTotal(pmt))
}

// totalMatchesLines adds the lines up with all their decimals, so the
// result doesn't depend on their order.
func totalMatchesLines(val any) bool {
	pmt, ok := val.(*bill.Payment)
	if !ok {
		return true
	}
	var exp uint32
	for _, l := range pmt.Lines {
		if l != nil {
			exp = max(exp, l.Amount.Exp())
		}
	}
	sum := num.MakeAmount(0, exp)
	for _, l := range pmt.Lines {
		switch {
		case l == nil:
		case l.Refund:
			sum = sum.Subtract(l.Amount)
		default:
			sum = sum.Add(l.Amount)
		}
	}
	return sum.Equals(pmt.Total)
}

// methodsOnIssueDate is checked on the payment, as a method's date is
// compared with the payment's own.
func methodsOnIssueDate(val any) bool {
	pmt, ok := val.(*bill.Payment)
	if !ok {
		return true
	}
	for _, m := range pmt.Methods {
		if m != nil && m.Date != nil && *m.Date != pmt.IssueDate {
			return false
		}
	}
	return true
}

// prepaidDrawnFits leaves missing exchange rates to rule 02 and to GOBL's
// check that the methods add up to the total.
func prepaidDrawnFits(val any) bool {
	pmt, ok := val.(*bill.Payment)
	if !ok {
		return true
	}
	return withinMaxAmount(PrepaidDrawn(pmt))
}

// prepaidLoadedFits leaves a missing exchange rate to rule 02.
func prepaidLoadedFits(val any) bool {
	pmt, ok := val.(*bill.Payment)
	if !ok {
		return true
	}
	return withinMaxAmount(PrepaidLoaded(pmt))
}

// withinMaxAmount says whether a missing amount, or one between the smallest
// and largest EET takes, fits; it compares with both, as negating the
// smallest int64 overflows.
func withinMaxAmount(a *num.Amount) bool {
	return a == nil || (a.Compare(maxAmount) <= 0 && a.Compare(minAmount) >= 0)
}
