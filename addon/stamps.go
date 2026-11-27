package addon

import (
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
)

// StampPOK is the confirmation code (potvrzovací kód, POK) the EET service
// returns when it records a sale.
const StampPOK cbc.Key = "eet-pok"

var stamps = []*cbc.Definition{
	{
		Key: StampPOK,
		Name: i18n.String{
			i18n.EN: "EET: Confirmation Code (POK)",
			i18n.CS: "EET: Potvrzovací kód (POK)",
		},
		Desc: i18n.String{
			i18n.EN: "Confirmation code the Financial Administration returns when it records the sale. A payment with a POK for the environment it would be sent to is not sent again, and a Playground POK, ending in -ff, doesn't count in production.",
		},
		Usage: &cbc.Usage{
			Keys: []cbc.Key{V1},
		},
	},
}
