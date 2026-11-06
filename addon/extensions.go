package addon

import (
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/pkg/here"
)

// Extension keys that say where a sale happened. Both go in the invoice's
// tax extensions.
const (
	// ExtKeyUnit is the recording unit (evidenční jednotka) of the sale.
	ExtKeyUnit cbc.Key = "cz-eet-unit"
	// ExtKeyDevice is the end device (pokladní zařízení) of the sale.
	ExtKeyDevice cbc.Key = "cz-eet-device"
)

var extensionUsage = &cbc.Usage{
	Keys:    []cbc.Key{V1},
	Schemas: []string{bill.ShortSchemaTax},
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

				~~~js
				"tax": {
					"ext": {
						"cz-eet-unit": "303",
						"cz-eet-device": "POS-1"
					}
				}
				~~~
			`),
		},
		Pattern: `^[1-9][0-9]{0,8}$`,
		Usage:   extensionUsage,
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
		Usage:   extensionUsage,
	},
}
