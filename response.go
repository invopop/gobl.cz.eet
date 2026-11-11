package eet

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"

	"github.com/beevik/etree"
)

// SOAPAction is the action of the service's one operation, OdeslaniTrzby.
// SOAP 1.1 sends it in quotes: SOAPAction: "http://fs.gov.cz/eet/OdeslaniTrzby".
const SOAPAction = "http://fs.gov.cz/eet/OdeslaniTrzby"

// Response (Odpoved) is the service's answer to a sale message: a
// confirmation with the POK when the sale is recorded, or an error, and up
// to ten warnings either way.
type Response struct {
	Header       *ResponseHeader `xml:"http://fs.gov.cz/eet/schema/v4 Hlavicka"`
	Confirmation *Confirmation   `xml:"http://fs.gov.cz/eet/schema/v4 Potvrzeni"`
	Error        *Error          `xml:"http://fs.gov.cz/eet/schema/v4 Chyba"`
	Warnings     []*Warning      `xml:"http://fs.gov.cz/eet/schema/v4 Varovani"`
}

// ResponseHeader (Hlavicka) of a response.
type ResponseHeader struct {
	// UUID (uuid_zpravy) of the message answered.
	UUID string `xml:"uuid_zpravy,attr"`
	// ReceivedAt (dat_prij) is when the sale was recorded.
	ReceivedAt string `xml:"dat_prij,attr"`
	// RejectedAt (dat_odmit) is when the message was rejected.
	RejectedAt string `xml:"dat_odmit,attr"`
}

// Confirmation (Potvrzeni) that the sale was recorded.
type Confirmation struct {
	// POK (pok) is the confirmation code. A resend gets a new one, so keep
	// the first.
	POK string `xml:"pok,attr"`
	// Test (test) is true outside production.
	Test bool `xml:"test,attr"`
}

// UnmarshalXML reads a Potvrzeni element, refusing one without a POK
// rather than giving an empty confirmation code.
func (c *Confirmation) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw struct {
		POK  string `xml:"pok,attr"`
		Test bool   `xml:"test,attr"`
	}
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	if raw.POK == "" {
		return errors.New("eet: confirmation without a POK")
	}
	*c = Confirmation{POK: raw.POK, Test: raw.Test}
	return nil
}

// Error (Chyba) explains why a sale was not recorded. In verification mode,
// code 0 says the message passed every check.
type Error struct {
	Code    ErrorCode `xml:"kod,attr"`
	Test    bool      `xml:"test,attr"`
	Message string    `xml:",chardata"`
}

// Error gives the code and the service's text.
func (e *Error) Error() string {
	return fmt.Sprintf("eet: error %d: %s", e.Code, e.Message)
}

// UnmarshalXML reads a Chyba element, refusing one without a numeric code:
// a missing code would otherwise read as 0, the verification success.
func (e *Error) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw struct {
		Code    string `xml:"kod,attr"`
		Test    bool   `xml:"test,attr"`
		Message string `xml:",chardata"`
	}
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	code, err := strconv.Atoi(raw.Code)
	if err != nil {
		return fmt.Errorf("eet: error code %q is not a number", raw.Code)
	}
	*e = Error{Code: ErrorCode(code), Test: raw.Test, Message: raw.Message}
	return nil
}

// Warning (Varovani) about a sale that was recorded all the same.
type Warning struct {
	Code    WarningCode `xml:"kod_varov,attr"`
	Message string      `xml:",chardata"`
}

// ErrorCode (kod) of a response. Match on the code, not on the text, which
// differs between versions of the spec.
type ErrorCode int

// Error codes of the interface description, chapter 3.5. Code 1 does not
// exist and 5 was used before version 4 only.
const (
	// ErrorCodeTemporary is a temporary technical error: send again later.
	ErrorCodeTemporary ErrorCode = -1
	// ErrorCodeVerified says, in verification mode, that the message passed
	// every check.
	ErrorCodeVerified ErrorCode = 0
	// ErrorCodeEncoding says the XML encoding is not valid.
	ErrorCodeEncoding ErrorCode = 2
	// ErrorCodeSchema says the message fails the XML schema.
	ErrorCodeSchema ErrorCode = 3
	// ErrorCodeSignature says the signature is invalid, or the certificate is
	// not trusted, not valid or revoked.
	ErrorCodeSignature ErrorCode = 4
	// ErrorCodeTaxID says the merchant's tax ID (eic_popl) has a wrong
	// structure.
	ErrorCodeTaxID ErrorCode = 6
	// ErrorCodeTooLarge says the message is over 12 kB.
	ErrorCodeTooLarge ErrorCode = 7
	// ErrorCodeProcessing is a technical or data error in processing: send
	// again later.
	ErrorCodeProcessing ErrorCode = 8
)

// Retry reports whether the sale can be sent again later as it is, as a
// resend with a new message (codes -1 and 8). The others need the data or
// the setup fixed first.
func (c ErrorCode) Retry() bool {
	return c == ErrorCodeTemporary || c == ErrorCodeProcessing
}

// WarningCode (kod_varov) of a response.
type WarningCode int

// Warning codes of the interface description, chapter 3.4.
const (
	// WarningCodeTaxIDMismatch says the merchant's tax ID differs from the
	// CN of the signing certificate.
	WarningCodeTaxIDMismatch WarningCode = 1
	// WarningCodeDelegatingTaxID says the delegating merchant's tax ID has a
	// wrong structure.
	WarningCodeDelegatingTaxID WarningCode = 2
	// WarningCodeSaleTimeAhead says the sale time is more than 2 hours after
	// the message was received.
	WarningCodeSaleTimeAhead WarningCode = 4
	// WarningCodeSaleTimeOld says the sale time is more than 2 years before
	// the message was received, or before the environment's first day.
	WarningCodeSaleTimeOld WarningCode = 5
	// WarningCodeUnitID says the unit ID doesn't have the form DIS+ gives
	// units.
	WarningCodeUnitID WarningCode = 6
	// WarningCodeSelfDelegation says the delegating merchant is the merchant
	// itself.
	WarningCodeSelfDelegation WarningCode = 7
)

// ParseResponse reads the service's SOAP response to a sale. A confirmation
// must be signed by a certificate the trust accepts, or ParseResponse fails:
// the POK is only worth keeping when it's genuine. Errors come unsigned, as
// the service sends them. Checking that the response answers the message
// sent, by Header.UUID, is up to the caller.
func ParseResponse(data []byte, trust *Trust) (*Response, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, fmt.Errorf("eet: reading response: %w", err)
	}
	body := child(doc.Root(), nsSOAP, "Body")
	if fault := child(body, nsSOAP, "Fault"); fault != nil {
		return nil, fmt.Errorf("eet: SOAP fault %s: %s", text(child(fault, "", "faultcode")), text(child(fault, "", "faultstring")))
	}
	answer := child(body, NamespaceEET, "Odpoved")
	if answer == nil {
		return nil, errors.New("eet: response has no Odpoved")
	}

	// Read the answer from the canonical body, the bytes a signature covers:
	// not from the whole envelope, where a second body could hide, and
	// without the namespace declarations canonicalization leaves out, which
	// the decoder could take for attributes.
	signed := child(answer, NamespaceEET, "Potvrzeni") != nil
	var canonical []byte
	if signed {
		cert, verified, err := verifySignature(doc)
		if err != nil {
			return nil, err
		}
		if err := trust.check(cert); err != nil {
			return nil, err
		}
		canonical = verified
	} else {
		var err error
		if canonical, err = canonicalize(body, ""); err != nil {
			return nil, err
		}
	}
	var envelopeBody struct {
		Response *Response `xml:"http://fs.gov.cz/eet/schema/v4 Odpoved"`
	}
	if err := xml.Unmarshal(canonical, &envelopeBody); err != nil {
		return nil, fmt.Errorf("eet: reading response: %w", err)
	}
	res := envelopeBody.Response
	switch {
	case res == nil:
		return nil, errors.New("eet: response has no Odpoved")
	case res.Confirmation != nil && !signed:
		return nil, errors.New("eet: confirmation is not signed")
	case res.Confirmation != nil && res.Confirmation.Test != trust.Playground:
		return nil, fmt.Errorf("eet: confirmation from %s, expected %s", environment(res.Confirmation.Test), environment(trust.Playground))
	case res.Confirmation == nil && res.Error == nil:
		return nil, errors.New("eet: response has neither a confirmation nor an error")
	}
	return res, nil
}

func environment(test bool) string {
	if test {
		return "the Playground"
	}
	return "production"
}
