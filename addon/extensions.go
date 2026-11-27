package addon

import (
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/pkg/here"
)

// Extension keys of the addon.
const (
	// ExtKeyUnit is the recording unit (evidenční jednotka) of the sale, in
	// the payment's extensions.
	ExtKeyUnit cbc.Key = "cz-eet-unit"
	// ExtKeyDevice is the end device (pokladní zařízení) of the sale, in the
	// payment's extensions.
	ExtKeyDevice cbc.Key = "cz-eet-device"
	// ExtKeyMultipleTaxpayers says, in the payment's extensions, whether the
	// sale is income of several taxpayers.
	ExtKeyMultipleTaxpayers cbc.Key = "cz-eet-multiple-taxpayers"
	// ExtKeyPrepaidLoad says, in a payment line's extensions, whether the line
	// loads prepaid credit.
	ExtKeyPrepaidLoad cbc.Key = "cz-eet-prepaid-load"
)

// Codes of the extensions that say yes or no. A missing extension counts as
// ExtCodeFalse.
const (
	ExtCodeTrue  cbc.Code = "true"
	ExtCodeFalse cbc.Code = "false"
)

var paymentExtensionUsage = &cbc.Usage{
	Keys:    []cbc.Key{V1},
	Schemas: []string{bill.ShortSchemaPayment},
}

var yesNo = []*cbc.Definition{
	{
		Code: ExtCodeTrue,
		Name: i18n.String{
			i18n.EN: "Yes",
			i18n.CS: "Ano",
		},
	},
	{
		Code: ExtCodeFalse,
		Name: i18n.String{
			i18n.EN: "No",
			i18n.CS: "Ne",
		},
	},
}

var extensions = []*cbc.Definition{
	{
		Key: ExtKeyUnit,
		Name: i18n.String{
			i18n.EN: "EET: Recording Unit",
			i18n.CS: "EET: Evidenční jednotka",
		},
		Desc: i18n.String{
			i18n.EN: here.Doc(`
				Number of the recording unit (evidenční jednotka) where the sale happened,
				such as a shop, a stall or an e-shop, as the merchant registered it in the
				Financial Administration's DIS+ portal. Sent as ~id_jednotky~.

				DIS+ numbers have at least two digits and end in 1, 2, 3 or 4. EET also
				records a sale with any other number from 1 to 999999999, and adds
				warning 6 to its confirmation.

				~~~js
				"ext": {
					"cz-eet-unit": "303",
					"cz-eet-device": "POS-1"
				}
				~~~
			`),
		},
		Pattern: `^[1-9][0-9]{0,8}$`,
		Usage:   paymentExtensionUsage,
	},
	{
		Key: ExtKeyDevice,
		Name: i18n.String{
			i18n.EN: "EET: Device",
			i18n.CS: "EET: Pokladní zařízení",
		},
		Desc: i18n.String{
			i18n.EN: here.Doc(`
				Identifier of the end device (pokladní zařízení) that took the payment,
				unique within its recording unit: up to 20 letters, digits, spaces or
				~. , : ; / # - _~. Every device needs its own identifier, also when a
				shared server sends the messages. Sent as ~id_pokl~.
			`),
		},
		Pattern: `^[0-9a-zA-Z\.,:;/#\-_ ]{1,20}$`,
		Usage:   paymentExtensionUsage,
	},
	{
		Key: ExtKeyMultipleTaxpayers,
		Name: i18n.String{
			i18n.EN: "EET: Income of Several Taxpayers",
			i18n.CS: "EET: Tržba více poplatníků",
		},
		Desc: i18n.String{
			i18n.EN: here.Doc(`
				Whether the sale is income of several taxpayers, sent as
				~povereni_vice_popl~ (item description, chapter 3, situation 5). With an
				ordering issuer, a third party records it for them; without one, the
				supplier records it for all of them. Without the extension, the sale is
				income of the supplier alone.
			`),
		},
		Values: yesNo,
		Usage:  paymentExtensionUsage,
	},
	{
		Key: ExtKeyPrepaidLoad,
		Name: i18n.String{
			i18n.EN: "EET: Prepaid Credit Loaded",
			i18n.CS: "EET: Nabití předplaceného kreditu",
		},
		Desc: i18n.String{
			i18n.EN: here.Doc(`
				Whether the payment line loads prepaid credit in a limited network, such
				as a wallet topped up or a voucher sold. The lines marked ~true~ add up,
				refund lines subtracted, to the part of the total sent as
				~urceno_cerp_zuct~ (item 13), left out when it is zero. A load and a
				purchase paid together (item 14, example 1) are one payment with a line
				for each. Without the extension, the line doesn't load credit.

				A gift voucher for a named product or service is not prepaid credit: the
				sale is recorded when the voucher is sold, and using it records nothing
				(item 14, example 4).

				~~~js
				"lines": [
					{
						"amount": "1000.00",
						"ext": { "cz-eet-prepaid-load": "true" }
					},
					{ "amount": "50.00" }
				]
				~~~
			`),
		},
		Values: yesNo,
		Usage: &cbc.Usage{
			Keys:    []cbc.Key{V1},
			Schemas: []string{bill.ShortSchemaPaymentLine},
		},
	},
}
