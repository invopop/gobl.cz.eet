package eet

import "encoding/xml"

// NamespaceEET is the namespace of the EET 2.0 messages, schema version 4.
const NamespaceEET = "http://fs.gov.cz/eet/schema/v4"

// Sale is the data message of a recorded sale (Trzba), the one request the
// EET service accepts. Values are kept as they go on the wire: the
// converter formats amounts and times, and optional attributes that don't
// apply stay empty so they're left out, as the service rejects empty ones.
type Sale struct {
	XMLName xml.Name `xml:"v4:Trzba"`
	Header  *Header  `xml:"v4:Hlavicka"`
	Data    *Data    `xml:"v4:Data"`
}

// Header (Hlavicka) identifies one attempt to send a sale.
type Header struct {
	// UUID (uuid_zpravy) of the message, new for every attempt, resends
	// included.
	UUID string `xml:"uuid_zpravy,attr"`
	// SentAt (dat_odesl) is when the message was sent.
	SentAt string `xml:"dat_odesl,attr"`
	// FirstAttempt (prvni_zaslani) is false on a resend.
	FirstAttempt bool `xml:"prvni_zaslani,attr"`
	// Verification (overeni) asks the service to check the message without
	// recording it: the response has error code 0 and no POK.
	Verification *bool `xml:"overeni,attr,omitempty"`
}

// Data (Data) describes the sale itself.
type Data struct {
	// TaxID (eic_popl) of the merchant, the CN of the signing certificate.
	TaxID string `xml:"eic_popl,attr"`
	// DelegatingTaxID (eic_poverujiciho) of the merchant that delegated the
	// recording, when there is one.
	DelegatingTaxID string `xml:"eic_poverujiciho,attr,omitempty"`
	// DelegatedByMany (povereni_vice_popl) when several merchants delegated
	// the recording.
	DelegatedByMany bool `xml:"povereni_vice_popl,attr,omitempty"`
	// UnitID (id_jednotky) of the recording unit, as registered in DIS+.
	UnitID string `xml:"id_jednotky,attr"`
	// DeviceID (id_pokl) of the end device within the unit.
	DeviceID string `xml:"id_pokl,attr"`
	// Number (porad_cis) of the sale in the device's sequence.
	Number string `xml:"porad_cis,attr"`
	// SoldAt (dat_trzby) is the local time of the sale.
	SoldAt string `xml:"dat_trzby,attr"`
	// Total (celk_trzba) amount paid, in CZK.
	Total string `xml:"celk_trzba,attr"`
	// PrepaidLoad (urceno_cerp_zuct) is the part of the total meant to be
	// drawn down or settled later, such as a voucher sold.
	PrepaidLoad string `xml:"urceno_cerp_zuct,attr,omitempty"`
	// PrepaidUse (cerp_zuct) is the part paid by drawing down an earlier
	// prepayment.
	PrepaidUse string `xml:"cerp_zuct,attr,omitempty"`
}
