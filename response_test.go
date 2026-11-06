package eet

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures in test/responses are the Playground's replies to messages
// signed with its CZ00000019 test certificate, on 6 November 2026.

func TestParseConfirmation(t *testing.T) {
	res, err := ParseResponse(readResponse(t, "confirmation"), playgroundTrust())
	require.NoError(t, err)
	assert.Equal(t, &Response{
		Header: &ResponseHeader{
			UUID:       "f1263599-5a52-495a-a9a9-4e1e3b3efeb4",
			ReceivedAt: "2026-11-06T12:18:27+01:00",
		},
		Confirmation: &Confirmation{
			POK:  "9765cced-b373-4a13-bbdc-986a62bc3544-ff",
			Test: true,
		},
	}, res)
}

func TestParseConfirmationWithWarning(t *testing.T) {
	res, err := ParseResponse(readResponse(t, "warning-unit"), playgroundTrust())
	require.NoError(t, err)
	assert.Equal(t, "6ccd7f8f-6868-4882-b5ca-986a62bc6fce-ff", res.Confirmation.POK)
	assert.Equal(t, []*Warning{
		{Code: WarningCodeUnitID, Message: "id_jednotky neodpovida formatem pridelenemu c. evidencni jednotky"},
	}, res.Warnings)
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		uuid string
	}{
		{
			name: "verification",
			err:  &Error{Code: ErrorCodeVerified, Test: true, Message: "Datovou zpravu evidovane trzby v overovacim modu se podarilo zpracovat"},
			uuid: "97e1a938-b18e-4adb-a5b8-2e1f6a08ce15",
		},
		{
			name: "error-schema",
			err:  &Error{Code: ErrorCodeSchema, Test: true, Message: "XML zprava nevyhovela kontrole XML schematu"},
			uuid: "6c21a587-2dcd-4c99-9755-6347f02ac60b",
		},
		{
			name: "error-signature",
			err:  &Error{Code: ErrorCodeSignature, Test: true, Message: "Neplatny podpis SOAP zpravy"},
			uuid: "03965780-6457-4842-bd80-5f9195c0b8c8",
		},
		{
			name: "error-tax-id",
			err:  &Error{Code: ErrorCodeTaxID, Test: true, Message: "EIC poplatnika ma chybnou strukturu"},
			uuid: "43bbaace-5bbc-4ccb-b6e7-9c89840b3ca7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ParseResponse(readResponse(t, tt.name), playgroundTrust())
			require.NoError(t, err)
			assert.Nil(t, res.Confirmation)
			assert.Equal(t, tt.err, res.Error)
			assert.Equal(t, tt.uuid, res.Header.UUID)
			assert.NotEmpty(t, res.Header.RejectedAt)
		})
	}
}

func TestParseRejectsUntrustedConfirmations(t *testing.T) {
	confirmation := string(readResponse(t, "confirmation"))
	tests := []struct {
		name  string
		data  string
		trust func() *Trust
		err   string
	}{
		{
			name: "changed POK",
			data: strings.Replace(confirmation, `pok="9765cced`, `pok="9765ccee`, 1),
			err:  "eet: digest does not match the SOAP body",
		},
		{
			name: "no signature",
			data: confirmation[:strings.Index(confirmation, "<Signature ")] + confirmation[strings.Index(confirmation, "</Signature>")+len("</Signature>"):],
			err:  "eet: message has no signature",
		},
		{
			name: "another root",
			trust: func() *Trust {
				tr := playgroundTrust()
				_, cert := newTestCertificate(t)
				tr.Roots = x509.NewCertPool()
				tr.Roots.AddCert(cert)
				return tr
			},
			err: "eet: response signed by an untrusted certificate: x509: certificate signed by unknown authority",
		},
		{
			name: "another organisation",
			trust: func() *Trust {
				tr := playgroundTrust()
				tr.Organization = "Správa státních služeb vytvářejících důvěru"
				return tr
			},
			err: `eet: response signed by ["Generální finanční ředitelství"], not "Správa státních služeb vytvářejících důvěru"`,
		},
		{
			name: "expired certificate",
			trust: func() *Trust {
				tr := playgroundTrust()
				tr.now = func() time.Time { return time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC) }
				return tr
			},
			err: "eet: response signed by an untrusted certificate: x509: certificate has expired or is not yet valid",
		},
		{
			name:  "no roots",
			trust: func() *Trust { return &Trust{Organization: OrganizationGFR} },
			err:   "eet: no trusted roots to check the response against",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, trust := tt.data, playgroundTrust()
			if data == "" {
				data = confirmation
			}
			if tt.trust != nil {
				trust = tt.trust()
			}
			_, err := ParseResponse([]byte(data), trust)
			assert.ErrorContains(t, err, tt.err)
		})
	}
}

// A second body with another POK is never read: after the signed body it is
// ignored, and before it the signature no longer references the body read.
func TestParseReadsTheSignedBody(t *testing.T) {
	confirmation := string(readResponse(t, "confirmation"))
	forged := `<soapenv:Body><eet:Odpoved><eet:Hlavicka uuid_zpravy="f1263599-5a52-495a-a9a9-4e1e3b3efeb4"/>` +
		`<eet:Potvrzeni pok="11111111-1111-4111-8111-111111111111-ff"/></eet:Odpoved></soapenv:Body>`

	after := strings.Replace(confirmation, "</soapenv:Body>", "</soapenv:Body>"+forged, 1)
	res, err := ParseResponse([]byte(after), playgroundTrust())
	require.NoError(t, err)
	assert.Equal(t, "9765cced-b373-4a13-bbdc-986a62bc3544-ff", res.Confirmation.POK)

	before := strings.Replace(confirmation, "<soapenv:Body ", forged+"<soapenv:Body ", 1)
	_, err = ParseResponse([]byte(before), playgroundTrust())
	assert.EqualError(t, err, "eet: signature must reference the SOAP body")
}

// A namespace declaration named like an attribute is left out of the
// canonical form, so the signature still holds, but a decoder matching
// attributes by local name would take it for the attribute.
func TestParseIgnoresUnsignedDeclarations(t *testing.T) {
	confirmation := string(readResponse(t, "confirmation"))
	tests := map[string]string{
		"POK": strings.Replace(confirmation, `test="true"/>`,
			`test="true" xmlns:pok="11111111-1111-4111-8111-111111111111-ff"/>`, 1),
		"UUID": strings.Replace(confirmation, `dat_prij="2026-11-06T12:18:27+01:00"/>`,
			`dat_prij="2026-11-06T12:18:27+01:00" xmlns:uuid_zpravy="22222222-2222-4222-8222-222222222222"/>`, 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, confirmation, data)
			res, err := ParseResponse([]byte(data), playgroundTrust())
			require.NoError(t, err)
			assert.Equal(t, "9765cced-b373-4a13-bbdc-986a62bc3544-ff", res.Confirmation.POK)
			assert.Equal(t, "f1263599-5a52-495a-a9a9-4e1e3b3efeb4", res.Header.UUID)
		})
	}
	t.Run("error code", func(t *testing.T) {
		data := strings.Replace(string(readResponse(t, "error-schema")), `kod="3"`, `kod="3" xmlns:kod="0"`, 1)
		res, err := ParseResponse([]byte(data), playgroundTrust())
		require.NoError(t, err)
		assert.Equal(t, ErrorCodeSchema, res.Error.Code)
	})
}

func TestParseMalformedResponses(t *testing.T) {
	tests := []struct {
		name string
		data string
		err  string
	}{
		{
			name: "SOAP fault",
			data: `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault>` +
				`<faultcode>soapenv:Server</faultcode><faultstring>Internal Error</faultstring></soapenv:Fault></soapenv:Body></soapenv:Envelope>`,
			err: "eet: SOAP fault soapenv:Server: Internal Error",
		},
		{
			name: "not XML",
			data: `<soapenv:Envelope`,
			err:  "eet: reading response: XML syntax error on line 1: unexpected EOF",
		},
		{
			name: "no answer",
			data: `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body/></soapenv:Envelope>`,
			err:  "eet: response has no Odpoved",
		},
		{
			name: "neither confirmation nor error",
			data: `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:eet="http://fs.gov.cz/eet/schema/v4">` +
				`<soapenv:Body><eet:Odpoved><eet:Hlavicka uuid_zpravy="f1263599-5a52-495a-a9a9-4e1e3b3efeb4"/></eet:Odpoved></soapenv:Body></soapenv:Envelope>`,
			err: "eet: response has neither a confirmation nor an error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResponse([]byte(tt.data), playgroundTrust())
			assert.EqualError(t, err, tt.err)
		})
	}
}

func TestErrorCodeRetry(t *testing.T) {
	retry := map[ErrorCode]bool{
		ErrorCodeTemporary:  true,
		ErrorCodeVerified:   false,
		ErrorCodeEncoding:   false,
		ErrorCodeSchema:     false,
		ErrorCodeSignature:  false,
		ErrorCodeTaxID:      false,
		ErrorCodeTooLarge:   false,
		ErrorCodeProcessing: true,
	}
	for code, expected := range retry {
		assert.Equal(t, expected, code.Retry(), code)
	}
}

func TestErrorMessage(t *testing.T) {
	err := &Error{Code: ErrorCodeSignature, Message: "Neplatny podpis SOAP zpravy"}
	assert.EqualError(t, err, "eet: error 4: Neplatny podpis SOAP zpravy")
}

// The embedded certificates are the ones whose fingerprints ca/README.md
// lists.
func TestDefaultTrustCertificates(t *testing.T) {
	fingerprints := map[string][]byte{
		"d279c01a12e8dd9a6230e459faa447ceb336998477338c2ee4135c96737418eb": icaRoot,
		"df5baf6d7e1a7d14e9911c5b8c676ec6ebcad9354a74f4ac7314e133e07a94de": icaPublic,
	}
	for fingerprint, data := range fingerprints {
		block, _ := pem.Decode(data)
		require.NotNil(t, block)
		sum := sha256.Sum256(block.Bytes)
		assert.Equal(t, fingerprint, hex.EncodeToString(sum[:]))
	}
	tr := DefaultTrust()
	assert.Equal(t, OrganizationGFR, tr.Organization)
	assert.Nil(t, tr.now)
}

// playgroundTrust checks certificates on the day the fixtures were made,
// so that the tests outlive the Playground certificate.
func playgroundTrust() *Trust {
	tr := DefaultTrust()
	tr.now = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }
	return tr
}

func readResponse(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("test/responses/" + name + ".xml")
	require.NoError(t, err)
	return data
}
