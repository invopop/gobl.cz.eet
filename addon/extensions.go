package addon

import (
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/pkg/here"
)

// Extension keys that say where a sale happened. Both go in the payment's
// extensions.
const (
	// ExtKeyUnit is the recording unit (evidenční jednotka) of the sale.
	ExtKeyUnit cbc.Key = "cz-eet-unit"
	// ExtKeyDevice is the end device (pokladní zařízení) of the sale.
	ExtKeyDevice cbc.Key = "cz-eet-device"
)

// ExtKeyDelegation says the payment is recorded under a delegation to record
// sales (pověření, § 10 of the EET Act).
const ExtKeyDelegation cbc.Key = "cz-eet-delegation"

// Values of ExtKeyDelegation.
const (
	// DelegationOne: the ordering issuer records the sale for the supplier,
	// who is the only taxpayer it is income of.
	DelegationOne cbc.Code = "one"
	// DelegationMany: the sale is income of several taxpayers, and the
	// ordering issuer, or the supplier when there is none, records it for all
	// of them.
	DelegationMany cbc.Code = "many"
)

// ExtKeyPrepaid marks prepaid credit: a payment method that draws on it, or
// a payment that loads it.
const ExtKeyPrepaid cbc.Key = "cz-eet-prepaid"

// Values of ExtKeyPrepaid.
const (
	// PrepaidDraw goes on a payment method that draws on prepaid credit.
	PrepaidDraw cbc.Code = "draw"
	// PrepaidLoad goes on a payment whose whole total loads prepaid credit.
	PrepaidLoad cbc.Code = "load"
)

var paymentExtensionUsage = &cbc.Usage{
	Keys:    []cbc.Key{V1},
	Schemas: []string{bill.ShortSchemaPayment},
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
		Key: ExtKeyPrepaid,
		Name: i18n.String{
			i18n.EN: "EET: Prepaid Credit",
			i18n.CS: "EET: Předplacený kredit",
		},
		Desc: i18n.String{
			i18n.EN: here.Doc(`
				Marks prepaid credit in a limited network, such as an electronic wallet or
				a voucher for goods or services not named on it.

				On a payment method, ~draw~ says the method draws on credit. The marked
				methods add up to the part of the total paid from credit, sent as
				~cerp_zuct~, negative in a refund.

				On the payment, ~load~ says its whole total loads credit, such as a
				wallet topped up or a voucher sold, and the total is sent as
				~urceno_cerp_zuct~ (item 13), left out when it is zero. A load and a
				purchase paid together (item 14, example 1) go as two receipts until
				GOBL's payment lines take extensions.

				A gift voucher for a named product or service is not prepaid credit: the
				sale is recorded when the voucher is sold, and using it records nothing
				(item 14, example 4).

				~~~js
				"methods": [
					{
						"key": "other",
						"amount": "200.00",
						"ext": {
							"cz-eet-prepaid": "draw"
						}
					}
				]
				~~~

				~~~js
				"ext": {
					"cz-eet-prepaid": "load"
				}
				~~~
			`),
		},
		Values: []*cbc.Definition{
			{
				Code: PrepaidDraw,
				Name: i18n.String{
					i18n.EN: "Drawn from prepaid credit",
					i18n.CS: "Čerpání předplaceného kreditu",
				},
			},
			{
				Code: PrepaidLoad,
				Name: i18n.String{
					i18n.EN: "Loaded onto prepaid credit",
					i18n.CS: "Nabití předplaceného kreditu",
				},
			},
		},
		Sources: []*cbc.Source{
			{
				Title: i18n.NewString("Finanční správa - Popis položek datové zprávy s příklady jejich vyplnění, verze 1.0"),
				URL:   "https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0",
			},
		},
		Usage: &cbc.Usage{
			Keys:    []cbc.Key{V1},
			Schemas: []string{bill.ShortSchemaPayment, pay.ShortSchemaRecord},
		},
	},
	{
		Key: ExtKeyDelegation,
		Name: i18n.String{
			i18n.EN: "EET: Delegation",
			i18n.CS: "EET: Pověření",
		},
		Desc: i18n.String{
			i18n.EN: here.Doc(`
				Says the payment is recorded under a delegation to record sales
				(pověření, § 10 of the EET Act), as the item description sets out in
				chapter 3, situations 4 and 5. The supplier is the taxpayer the sale is
				income of; the ordering issuer is the third party who records it.

				- ~one~ (situation 4): the ordering issuer records the sale for the
				  supplier, the only taxpayer it is income of. The issuer's tax ID is
				  sent as ~eic_popl~ and the supplier's as ~eic_poverujiciho~.
				- ~many~ (situation 5): the sale is income of several taxpayers, and
				  ~povereni_vice_popl~ is sent. With an ordering issuer, a third party
				  records it: the issuer's tax ID is sent as ~eic_popl~ and the
				  supplier's, one of the taxpayers, as ~eic_poverujiciho~, which the
				  Financial Administration recommends. Without one, the supplier
				  records it for all of them.

				Whoever records the sale uses their own recording unit and their own
				certificate, whose tax ID should be the one sent as ~eic_popl~, or the
				service adds warning 1. An ordering issuer without this extension is
				refused. The payee, who receives the money, isn't part of the message.
			`),
		},
		Values: []*cbc.Definition{
			{
				Code: DelegationOne,
				Name: i18n.String{
					i18n.EN: "Delegated by one taxpayer",
					i18n.CS: "Pověření jedním poplatníkem",
				},
			},
			{
				Code: DelegationMany,
				Name: i18n.String{
					i18n.EN: "Delegated by several taxpayers",
					i18n.CS: "Pověření více poplatníky",
				},
			},
		},
		Sources: []*cbc.Source{
			{
				Title: i18n.NewString("Finanční správa - Popis položek datové zprávy s příklady jejich vyplnění, verze 1.0"),
				URL:   "https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0",
			},
		},
		Usage: paymentExtensionUsage,
	},
}
