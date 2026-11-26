package addon

import (
	"github.com/invopop/gobl/cbc"
)

// MeansKeyPrepaid qualifies a payment method that draws on prepaid credit in
// a limited network, such as an electronic wallet or a voucher for goods or
// services not named on it, as in "other+prepaid" or "card+prepaid". The
// marked methods add up to the part of the total paid from credit, sent as
// cerp_zuct (item 14), negative in a refund.
const MeansKeyPrepaid cbc.Key = "prepaid"
