package addon

import (
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/pkg/here"
	"github.com/invopop/gobl/tax"
)

// Tags of a payment whose kind changes the EET message.
const (
	// TagPrepaidLoad marks a payment whose whole total loads prepaid credit.
	TagPrepaidLoad cbc.Key = "prepaid-load"
	// TagMultipleTaxpayers marks a sale that is income of several taxpayers.
	TagMultipleTaxpayers cbc.Key = "multiple-taxpayers"
)

var paymentTags = &tax.TagSet{
	Schema: bill.ShortSchemaPayment,
	List: []*cbc.Definition{
		{
			Key: TagPrepaidLoad,
			Name: i18n.String{
				i18n.EN: "Prepaid Credit Loaded",
				i18n.CS: "Nabití předplaceného kreditu",
			},
			Desc: i18n.String{
				i18n.EN: here.Doc(`
					The payment's whole total loads prepaid credit in a limited network,
					such as a wallet topped up or a voucher sold, and is sent as
					~urceno_cerp_zuct~ (item 13), left out when it is zero. A load and a
					purchase paid together (item 14, example 1) go as two receipts until
					GOBL's payment lines can say which of them is the load.

					A gift voucher for a named product or service is not prepaid credit: the
					sale is recorded when the voucher is sold, and using it records nothing
					(item 14, example 4).
				`),
			},
		},
		{
			Key: TagMultipleTaxpayers,
			Name: i18n.String{
				i18n.EN: "Income of Several Taxpayers",
				i18n.CS: "Tržba více poplatníků",
			},
			Desc: i18n.String{
				i18n.EN: here.Doc(`
					The sale is income of several taxpayers, and ~povereni_vice_popl~ is sent
					(item description, chapter 3, situation 5). With an ordering issuer, a
					third party records it for them; without one, the supplier records it
					for all of them.
				`),
			},
		},
	},
}
