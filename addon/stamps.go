package addon

import (
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
)

// StampPOK is the confirmation code (potvrzovací kód, POK) the EET service
// returns when it records a sale.
const StampPOK cbc.Key = "cz-eet-pok"

var stamps = []*cbc.Definition{
	{
		Key: StampPOK,
		Name: i18n.String{
			i18n.EN: "EET: Confirmation Code (POK)",
			i18n.CS: "EET: Potvrzovací kód (POK)",
		},
		Desc: i18n.String{
			i18n.EN: "Confirmation code the Financial Administration returns when it records the sale. The first one received is kept: a resend gets a new code.",
		},
		Usage: &cbc.Usage{
			Keys: []cbc.Key{V1},
		},
	},
}
