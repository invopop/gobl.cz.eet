package eet

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lestrrat-go/helium"
)

// Response (Odpoved) is the service's answer to a sale message: a
// confirmation with the POK when the sale is recorded, or an error, and up
// to ten warnings either way.
type Response struct {
	Header       *ResponseHeader `xml:"http://fs.gov.cz/eet/schema/v4 Hlavicka"`
	Confirmation *Confirmation   `xml:"http://fs.gov.cz/eet/schema/v4 Potvrzeni"`
	Error        *ResponseError  `xml:"http://fs.gov.cz/eet/schema/v4 Chyba"`
	Warnings     []*Warning      `xml:"http://fs.gov.cz/eet/schema/v4 Varovani"`

	// TransactionID is the X-Global-Transaction-Id the service tagged the
	// exchange with (interface description, 5.4), which only Send sees.
	TransactionID string `xml:"-"`
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

// UnmarshalXML reads a Hlavicka element, refusing attributes in another
// namespace.
func (h *ResponseHeader) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := checkAttrNamespaces(start); err != nil {
		return err
	}
	type plain ResponseHeader
	return d.DecodeElement((*plain)(h), &start)
}

// checkAttrNamespaces refuses an attribute in a namespace, as the decoder
// matches attributes by local name and would read x:kod as kod.
func checkAttrNamespaces(start xml.StartElement) error {
	for _, attr := range start.Attr {
		if attr.Name.Space != "" && attr.Name.Space != "xmlns" {
			return fmt.Errorf("%s has an attribute in another namespace: %s", start.Name.Local, attr.Name.Local)
		}
	}
	return nil
}

// Confirmation (Potvrzeni) that the sale was recorded.
type Confirmation struct {
	// POK (pok) is the confirmation code, unique to each confirmed message.
	POK string `xml:"pok,attr"`
	// Test (test) is true outside production.
	Test bool `xml:"test,attr"`
}

// UnmarshalXML reads a Potvrzeni element, refusing one without a POK.
func (c *Confirmation) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := checkAttrNamespaces(start); err != nil {
		return err
	}
	var raw struct {
		POK  string `xml:"pok,attr"`
		Test bool   `xml:"test,attr"`
	}
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	if raw.POK == "" {
		return errors.New("confirmation without a POK")
	}
	*c = Confirmation{POK: raw.POK, Test: raw.Test}
	return nil
}

// ResponseError (Chyba) explains why a sale was not recorded. In
// verification mode, code 0 says the message passed every check.
type ResponseError struct {
	Code    ErrorCode `xml:"kod,attr"`
	Test    bool      `xml:"test,attr"`
	Message string    `xml:",chardata"`
}

// Err maps the error by its code: nil for code 0, a verification that
// passed, ErrServer when the code says to send again later, ErrValidation
// otherwise.
func (e *ResponseError) Err() error {
	if e == nil || e.Code == ErrorCodeVerified {
		return nil
	}
	base := ErrValidation
	if e.Code.Retryable() {
		base = ErrServer
	}
	return base.WithCode(strconv.Itoa(int(e.Code))).WithMessage(e.Message)
}

// UnmarshalXML reads a Chyba element, refusing one without a numeric code:
// a missing code would otherwise read as 0, the verification success.
func (e *ResponseError) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := checkAttrNamespaces(start); err != nil {
		return err
	}
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
		return fmt.Errorf("error code %q is not a number", raw.Code)
	}
	*e = ResponseError{Code: ErrorCode(code), Test: raw.Test, Message: raw.Message}
	return nil
}

// Warning (Varovani) about a sale that was recorded all the same.
type Warning struct {
	Code    WarningCode `xml:"kod_varov,attr"`
	Message string      `xml:",chardata"`
}

// UnmarshalXML reads a Varovani element, refusing attributes in another
// namespace.
func (w *Warning) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := checkAttrNamespaces(start); err != nil {
		return err
	}
	type plain Warning
	return d.DecodeElement((*plain)(w), &start)
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

// Retryable reports whether the sale can be sent again later as it is: code
// 8, and every negative code, as -1 is a temporary error and -999 to -2 are
// reserved next to it (interface description, 3.5.4).
func (c ErrorCode) Retryable() bool {
	return c < 0 || c == ErrorCodeProcessing
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

// ParseResponse reads the service's reply to the sale, accepting a
// confirmation only when the environment's certificate signs it for that
// very message, and returns an error code other than 0 as an error alongside
// the reply.
func ParseResponse(data []byte, sale *Sale, env Environment) (*Response, error) {
	tr := env.trust()
	if tr == nil {
		return nil, ErrValidation.WithMessage(fmt.Sprintf("unknown environment %q", env))
	}
	return readReply(data, sale, tr)
}

// answerError is the error an answer stands for: none for a confirmation or
// a verification, and its code's error for any other error code.
func answerError(res *Response) error {
	if res.Error != nil {
		return res.Error.Err()
	}
	return nil
}

func parseResponse(data []byte, sale *Sale, tr *trust) (*Response, error) {
	if sale == nil || sale.Header == nil || sale.Header.UUID == "" {
		return nil, ErrValidation.WithMessage("a sale message with a UUID is needed to match the response")
	}
	doc, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	if doc.IntSubset() != nil || doc.ExtSubset() != nil {
		return nil, errors.New("response must not declare a document type (SOAP 1.1, section 3)")
	}
	root := doc.DocumentElement()
	if root.LocalName() != "Envelope" || root.URI() != nsSOAP {
		return nil, errors.New("response is not a SOAP envelope")
	}
	body, err := onlyChild(root, nsSOAP, "Body")
	if err != nil {
		return nil, err
	}
	if err := checkFault(body); err != nil {
		return nil, err
	}
	answer, err := onlyChild(body, nsEET, "Odpoved")
	if err != nil {
		return nil, err
	}
	if answer == nil {
		return nil, errors.New("response has no Odpoved")
	}

	// The answer's parts are decoded below, where a duplicate would silently
	// replace the first.
	signed := false
	for _, local := range []string{"Hlavicka", "Potvrzeni", "Chyba"} {
		el, err := onlyChild(answer, nsEET, local)
		if err != nil {
			return nil, err
		}
		if local == "Potvrzeni" {
			signed = el != nil
		}
	}
	var canonical []byte
	if signed {
		cert, verified, err := verifySignature(doc)
		if err != nil {
			return nil, err
		}
		if err := tr.check(cert); err != nil {
			return nil, err
		}
		canonical = verified
	} else {
		if canonical, err = canonicalBody(body, ""); err != nil {
			return nil, err
		}
	}
	// Decode the canonical body, the bytes a signature covers, not the raw
	// envelope.
	var decoded struct {
		Response *Response `xml:"http://fs.gov.cz/eet/schema/v4 Odpoved"`
	}
	if err := xml.Unmarshal(canonical, &decoded); err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	res := decoded.Response
	if err := checkAnswerShape(res, signed, tr.playground); err != nil {
		return nil, err
	}
	if err := checkAnswersSale(res, sale); err != nil {
		return nil, err
	}
	return res, nil
}

// checkAnswerShape checks that the answer has a confirmation or an error,
// not both, and that a confirmation is signed and from the environment.
func checkAnswerShape(res *Response, signed, playground bool) error {
	switch {
	case res == nil:
		return errors.New("response has no Odpoved")
	case res.Confirmation != nil && res.Error != nil:
		// OdpovedType is a choice of the two (EET schema).
		return errors.New("response has both a confirmation and an error")
	case res.Confirmation != nil && !signed:
		return errors.New("confirmation is not signed")
	case res.Confirmation != nil && res.Confirmation.Test != playground:
		return ErrEnvironment.WithMessage(fmt.Sprintf("confirmation from %s, expected %s", environmentName(res.Confirmation.Test), environmentName(playground)))
	case res.Confirmation == nil && res.Error == nil:
		return errors.New("response has neither a confirmation nor an error")
	}
	return nil
}

// checkAnswersSale checks that the response is for the sale's message and
// the mode it was sent in.
func checkAnswersSale(res *Response, sale *Sale) error {
	if err := checkAnswersUUID(res, sale.Header.UUID); err != nil {
		return err
	}
	if res.Error != nil && res.Error.Code == ErrorCodeVerified && !isVerification(sale) {
		// Code 0 only answers a message in verification mode (interface
		// description, 3.5 and table 1).
		return ErrConnection.WithMessage("error code 0 answers a message not in verification mode")
	}
	if res.Confirmation != nil && isVerification(sale) {
		// A message in verification mode is never recorded (interface
		// description, 2.1 and 2.2).
		return ErrConnection.WithMessage("confirmation answers a message in verification mode")
	}
	return nil
}

func isVerification(sale *Sale) bool {
	return sale.Header.Verification != nil && *sale.Header.Verification
}

// checkAnswersUUID checks that the response is for the message with the UUID
// given, whatever the letter case.
func checkAnswersUUID(res *Response, uuid string) error {
	var answered string
	if res.Header != nil {
		answered = res.Header.UUID
	}
	switch {
	case strings.EqualFold(answered, uuid):
		return nil
	case answered == "" && res.Confirmation == nil:
		// An error may leave out the UUID (interface description, 3.5.2).
		return nil
	}
	return ErrConnection.WithMessage(fmt.Sprintf("response answers another message: answers %q, sent %q", answered, uuid))
}

// checkFault returns the error a SOAP fault in the body stands for, or nil
// without one.
func checkFault(body *helium.Element) error {
	fault, err := onlyChild(body, nsSOAP, "Fault")
	if err != nil || fault == nil {
		return err
	}
	code, err := onlyChild(fault, "", "faultcode")
	if err != nil {
		return err
	}
	message, err := onlyChild(fault, "", "faultstring")
	if err != nil {
		return err
	}
	// A fault code is a QName, so the space around it doesn't count.
	return faultError(strings.TrimSpace(text(code)), text(message), faultCodeInSOAP(code))
}

// faultCodeInSOAP says whether the fault code's prefix, if it has one, is
// bound to the SOAP envelope namespace, where Server is defined.
func faultCodeInSOAP(code *helium.Element) bool {
	prefix, _, found := strings.Cut(strings.TrimSpace(text(code)), ":")
	if !found {
		return true
	}
	ns := helium.LookupNSByPrefix(code, prefix)
	return ns != nil && ns.URI() == nsSOAP
}

// faultError maps a SOAP fault by who caused it (SOAP 1.1, section 4.4.1):
// ErrServer for a Server fault code in the SOAP namespace, or a more specific
// one in its dot notation, such as Server.Busy, and ErrValidation for any
// other.
func faultError(code, message string, inSOAP bool) *Error {
	base := ErrValidation
	local := code
	if _, after, found := strings.Cut(code, ":"); found {
		local = after
	}
	if inSOAP && (local == "Server" || strings.HasPrefix(local, "Server.")) {
		base = ErrServer
	}
	return base.WithCode(code).WithMessage(message)
}

func environmentName(test bool) string {
	if test {
		return "the Playground"
	}
	return "production"
}
