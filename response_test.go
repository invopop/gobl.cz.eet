package eet

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures in test/responses are the Playground's replies to messages
// signed with its CZ00000019 test certificate, on 6 November 2026.

func TestParseConfirmation(t *testing.T) {
	res, err := parseResponse(readResponse(t, "confirmation"), sent(confirmationUUID), playgroundTrust())
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
	res, err := parseResponse(readResponse(t, "warning-unit"), sent("02f17749-1aff-4556-b485-fb87959ee350"), playgroundTrust())
	require.NoError(t, err)
	assert.Equal(t, "6ccd7f8f-6868-4882-b5ca-986a62bc6fce-ff", res.Confirmation.POK)
	assert.Equal(t, []*Warning{
		{Code: WarningCodeUnitID, Message: "id_jednotky neodpovida formatem pridelenemu c. evidencni jednotky"},
	}, res.Warnings)
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		err  *ResponseError
		uuid string
	}{
		{
			name: "verification",
			err:  &ResponseError{Code: ErrorCodeVerified, Test: true, Message: "Datovou zpravu evidovane trzby v overovacim modu se podarilo zpracovat"},
			uuid: "97e1a938-b18e-4adb-a5b8-2e1f6a08ce15",
		},
		{
			name: "error-schema",
			err:  &ResponseError{Code: ErrorCodeSchema, Test: true, Message: "XML zprava nevyhovela kontrole XML schematu"},
			uuid: "6c21a587-2dcd-4c99-9755-6347f02ac60b",
		},
		{
			name: "error-signature",
			err:  &ResponseError{Code: ErrorCodeSignature, Test: true, Message: "Neplatny podpis SOAP zpravy"},
			uuid: "03965780-6457-4842-bd80-5f9195c0b8c8",
		},
		{
			name: "error-tax-id",
			err:  &ResponseError{Code: ErrorCodeTaxID, Test: true, Message: "EIC poplatnika ma chybnou strukturu"},
			uuid: "43bbaace-5bbc-4ccb-b6e7-9c89840b3ca7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sale := sent(tt.uuid)
			if tt.err.Code == ErrorCodeVerified {
				sale.Header.Verification = ptr(true)
			}
			res, err := parseResponse(readResponse(t, tt.name), sale, playgroundTrust())
			require.NoError(t, err)
			assert.Nil(t, res.Confirmation)
			assert.Equal(t, tt.err, res.Error)
			assert.Equal(t, tt.uuid, res.Header.UUID)
			assert.NotEmpty(t, res.Header.RejectedAt)
		})
	}
}

// Error code 0 only answers a message in verification mode (interface
// description, 3.5): for any other, it is no answer to that message.
func TestParseVerificationCodeOnlyInVerificationMode(t *testing.T) {
	for name, flag := range map[string]*bool{"no flag": nil, "flag false": ptr(false)} {
		t.Run(name, func(t *testing.T) {
			sale := sent(verificationUUID)
			sale.Header.Verification = flag
			_, err := ParseResponse(readResponse(t, "verification"), sale, EnvironmentPlayground)
			assert.ErrorIs(t, err, ErrConnection)
			assert.EqualError(t, err, "connection: error code 0 answers a message not in verification mode")
		})
	}
	t.Run("verification mode", func(t *testing.T) {
		sale := sent(verificationUUID)
		sale.Header.Verification = ptr(true)
		res, err := ParseResponse(readResponse(t, "verification"), sale, EnvironmentPlayground)
		require.NoError(t, err)
		assert.Equal(t, ErrorCodeVerified, res.Error.Code)
	})
}

func TestParseRejectsUntrustedConfirmations(t *testing.T) {
	confirmation := string(readResponse(t, "confirmation"))
	tests := []struct {
		name  string
		data  string
		trust func() *trust
		err   string
	}{
		{
			name: "changed POK",
			data: strings.Replace(confirmation, `pok="9765cced`, `pok="9765ccee`, 1),
			err:  "invalid signature:",
		},
		{
			name: "no signature",
			data: confirmation[:strings.Index(confirmation, "<Signature ")] + confirmation[strings.Index(confirmation, "</Signature>")+len("</Signature>"):],
			err:  "message has no signature",
		},
		{
			name: "another root",
			trust: func() *trust {
				tr := playgroundTrust()
				_, cert := newTestCertificate(t)
				tr.roots = x509.NewCertPool()
				tr.roots.AddCert(cert)
				return tr
			},
			err: "untrusted: x509: certificate signed by unknown authority",
		},
		{
			name: "another organization expected",
			trust: func() *trust {
				tr := playgroundTrust()
				id := playgroundIdentity
				id.organization = "Správa státních služeb vytvářejících důvěru"
				tr.identity = id
				return tr
			},
			err: `untrusted: response signed by a certificate with organization "[Generální finanční ředitelství]", expected "Správa státních služeb vytvářejících důvěru"`,
		},
		{
			name: "expired certificate",
			trust: func() *trust {
				tr := playgroundTrust()
				tr.now = func() time.Time { return time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC) }
				return tr
			},
			err: "untrusted: x509: certificate has expired or is not yet valid",
		},
		{
			name:  "no roots",
			trust: func() *trust { return &trust{} },
			err:   "untrusted: no trusted roots to check the response against",
		},
		{
			name: "Playground reply in production",
			trust: func() *trust {
				tr := playgroundTrust()
				tr.playground, tr.identity = false, productionIdentity
				return tr
			},
			err: `untrusted: response signed by a certificate with common name "NEOSTRE - playground prostredi EET", expected "Spolecne technicke zarizeni spravce dane EET"`,
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
			_, err := parseResponse([]byte(data), sent(confirmationUUID), trust)
			assert.ErrorContains(t, err, tt.err)
			// Only a refused certificate or environment is ErrUntrusted or
			// ErrEnvironment; a broken signature is not.
			switch {
			case strings.HasPrefix(tt.err, "untrusted: "):
				assert.ErrorIs(t, err, ErrUntrusted)
				assert.NotErrorIs(t, err, ErrEnvironment)
			case strings.HasPrefix(tt.err, "environment: "):
				assert.ErrorIs(t, err, ErrEnvironment)
				assert.NotErrorIs(t, err, ErrUntrusted)
			default:
				assert.NotErrorIs(t, err, ErrUntrusted)
				assert.NotErrorIs(t, err, ErrEnvironment)
			}
		})
	}
}

// A reply with a second body, carrying another POK, is refused whichever
// body comes first: only a reply with exactly one body is read.
func TestParseRefusesASecondBody(t *testing.T) {
	confirmation := string(readResponse(t, "confirmation"))
	forged := `<soapenv:Body><eet:Odpoved><eet:Hlavicka uuid_zpravy="f1263599-5a52-495a-a9a9-4e1e3b3efeb4"/>` +
		`<eet:Potvrzeni pok="11111111-1111-4111-8111-111111111111-ff"/></eet:Odpoved></soapenv:Body>`
	errorSchema := string(readResponse(t, "error-schema"))
	tests := map[string]string{
		"after the signed body":  strings.Replace(confirmation, "</soapenv:Body>", "</soapenv:Body>"+forged, 1),
		"before the signed body": strings.Replace(confirmation, "<soapenv:Body ", forged+"<soapenv:Body ", 1),
		"in an unsigned error":   strings.Replace(errorSchema, "</soapenv:Body>", "</soapenv:Body>"+forged, 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseResponse([]byte(data), sent(confirmationUUID), playgroundTrust())
			assert.EqualError(t, err, "message has more than one Body")
		})
	}
}

// A second Odpoved in the one body, carrying a POK, is refused: an unsigned
// error must not turn into a confirmation.
func TestParseRefusesASecondAnswer(t *testing.T) {
	forged := `<eet:Odpoved><eet:Hlavicka uuid_zpravy="6c21a587-2dcd-4c99-9755-6347f02ac60b"/>` +
		`<eet:Potvrzeni pok="11111111-1111-4111-8111-111111111111-ff" test="true"/></eet:Odpoved>`
	errorSchema := string(readResponse(t, "error-schema"))
	tests := map[string]string{
		"after the error":  strings.Replace(errorSchema, "</eet:Odpoved>", "</eet:Odpoved>"+forged, 1),
		"before the error": strings.Replace(errorSchema, "<eet:Odpoved>", forged+"<eet:Odpoved>", 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := parseResponse([]byte(data), sent(errorSchemaUUID), playgroundTrust())
			assert.EqualError(t, err, "message has more than one Odpoved")
			assert.Nil(t, res)
		})
	}
}

// Changing a single byte anywhere in a genuine confirmation, by replacing
// it, deleting it or inserting a space before it, either gets the reply
// refused or reads the very same response: nothing outside what the
// signature covers can change what is read.
func TestParseSingleByteMutants(t *testing.T) {
	original := readResponse(t, "confirmation")
	sale, trust := sent(confirmationUUID), playgroundTrust()
	want, err := parseResponse(original, sale, trust)
	require.NoError(t, err)

	var mutants, accepted atomic.Int64
	check := func(kind string, at int, data []byte) {
		mutants.Add(1)
		got, err := parseResponse(data, sale, trust)
		if err != nil {
			return
		}
		accepted.Add(1)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s at byte %d read a different response: header %+v, confirmation %+v, error %+v, %d warnings",
				kind, at, got.Header, got.Confirmation, got.Error, len(got.Warnings))
		}
	}
	// The offsets are shared out between the CPUs; under the race detector
	// only every fourth one is checked.
	stride := 1
	if raceDetector {
		stride = 4
	}
	var wg sync.WaitGroup
	workers := runtime.GOMAXPROCS(0)
	for w := range workers {
		wg.Go(func() {
			for i := w * stride; i < len(original); i += workers * stride {
				replaced := bytes.Clone(original)
				replaced[i] = 'A'
				if original[i] == 'A' {
					replaced[i] = 'B'
				}
				check("replace", i, replaced)
				check("delete", i, slices.Concat(original[:i], original[i+1:]))
				check("insert", i, slices.Concat(original[:i], []byte(" "), original[i:]))
			}
		})
	}
	wg.Wait()
	t.Logf("%d mutants, %d accepted, all reading the original response", mutants.Load(), accepted.Load())
	assert.NotZero(t, accepted.Load(), "no mutant was accepted, so the comparison never ran")
}

// A genuine reply to another message, a replay or a crossed answer, must not
// confirm this sale.
func TestParseAnswerToAnotherMessage(t *testing.T) {
	other := sent("5a0c1e9d-8f2b-4d3a-9c47-1e6b2f8a0d35")

	t.Run("confirmation", func(t *testing.T) {
		_, err := parseResponse(readResponse(t, "confirmation"), other, playgroundTrust())
		assert.ErrorIs(t, err, ErrConnection)
		assert.ErrorContains(t, err, "response answers another message")
	})
	t.Run("error", func(t *testing.T) {
		_, err := parseResponse(readResponse(t, "error-schema"), other, playgroundTrust())
		assert.ErrorIs(t, err, ErrConnection)
		assert.ErrorContains(t, err, "response answers another message")
	})
	t.Run("error without a UUID", func(t *testing.T) {
		// The interface description (3.5.2) makes uuid_zpravy optional in an
		// error reply, so one without it is taken as it is.
		data := strings.Replace(string(readResponse(t, "error-schema")), `uuid_zpravy="`+errorSchemaUUID+`" `, ``, 1)
		require.NotContains(t, data, errorSchemaUUID)
		res, err := parseResponse([]byte(data), other, playgroundTrust())
		require.NoError(t, err)
		assert.Equal(t, ErrorCodeSchema, res.Error.Code)
	})
	t.Run("same UUID in capitals", func(t *testing.T) {
		res, err := parseResponse(readResponse(t, "confirmation"), sent(strings.ToUpper(confirmationUUID)), playgroundTrust())
		require.NoError(t, err)
		assert.Equal(t, "9765cced-b373-4a13-bbdc-986a62bc3544-ff", res.Confirmation.POK)
	})
	t.Run("no sale", func(t *testing.T) {
		_, err := parseResponse(readResponse(t, "confirmation"), nil, playgroundTrust())
		assert.EqualError(t, err, "validation: a sale message with a UUID is needed to match the response")
	})
}

// A namespace declaration named like an attribute is refused, as Canonical
// XML refuses the relative namespace URI it carries (C14N 1.0, section 2.3).
func TestParseRefusesNamespaceDeclarationsAsValues(t *testing.T) {
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
			res, err := parseResponse([]byte(data), sent(confirmationUUID), playgroundTrust())
			assert.ErrorContains(t, err, "relative namespace URI")
			assert.Nil(t, res)
		})
	}
	t.Run("error code", func(t *testing.T) {
		data := strings.Replace(string(readResponse(t, "error-schema")), `kod="3"`, `kod="3" xmlns:kod="0"`, 1)
		res, err := parseResponse([]byte(data), sent(errorSchemaUUID), playgroundTrust())
		assert.ErrorContains(t, err, "relative namespace URI")
		assert.Nil(t, res)
	})
}

// A namespace declaration with an absolute URI, named like an attribute,
// is left out of the canonical body, so the authentic values are read.
func TestParseIgnoresNamespaceDeclarationsNamedLikeAttributes(t *testing.T) {
	confirmation := string(readResponse(t, "confirmation"))
	original, err := parseResponse([]byte(confirmation), sent(confirmationUUID), playgroundTrust())
	require.NoError(t, err)
	tests := map[string]string{
		"POK": strings.Replace(confirmation, `test="true"/>`, `test="true" xmlns:pok="urn:forged-pok"/>`, 1),
		"UUID": strings.Replace(confirmation, `dat_prij="2026-11-06T12:18:27+01:00"/>`,
			`dat_prij="2026-11-06T12:18:27+01:00" xmlns:uuid_zpravy="urn:forged-uuid"/>`, 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, confirmation, data)
			res, err := parseResponse([]byte(data), sent(confirmationUUID), playgroundTrust())
			require.NoError(t, err)
			assert.Equal(t, original.Confirmation.POK, res.Confirmation.POK)
			assert.Equal(t, original.Header.UUID, res.Header.UUID)
		})
	}
	t.Run("error code", func(t *testing.T) {
		data := strings.Replace(string(readResponse(t, "error-schema")), `kod="3"`, `kod="3" xmlns:kod="urn:forged-code"`, 1)
		res, err := parseResponse([]byte(data), sent(errorSchemaUUID), playgroundTrust())
		require.NoError(t, err)
		assert.Equal(t, ErrorCodeSchema, res.Error.Code)
	})
}

// An attribute in another namespace, named like one of the answer's, is
// refused, as the decoder would read it for the attribute.
func TestParseRefusesNamespacedAttributes(t *testing.T) {
	tests := map[string]struct {
		fixture, uuid, from, to, element string
	}{
		"error code": {"error-schema", errorSchemaUUID, `kod="3"`, `xmlns:x="urn:x" x:kod="0" kod="3"`, "Chyba"},
		"UUID":       {"error-schema", errorSchemaUUID, `dat_odmit=`, `xmlns:x="urn:x" x:uuid_zpravy="22222222-2222-4222-8222-222222222222" dat_odmit=`, "Hlavicka"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			reply := string(readResponse(t, tt.fixture))
			data := strings.Replace(reply, tt.from, tt.to, 1)
			require.NotEqual(t, reply, data)
			res, err := parseResponse([]byte(data), sent(tt.uuid), playgroundTrust())
			assert.ErrorContains(t, err, tt.element+" has an attribute in another namespace")
			assert.Nil(t, res)
		})
	}
}

// An error without a code must not read as code 0, which in verification
// mode means success.
func TestParseErrorNeedsCode(t *testing.T) {
	errorSchema := string(readResponse(t, "error-schema"))
	tests := map[string]string{
		"missing":    strings.Replace(errorSchema, `kod="3" `, ``, 1),
		"empty":      strings.Replace(errorSchema, `kod="3"`, `kod=""`, 1),
		"not number": strings.Replace(errorSchema, `kod="3"`, `kod="tři"`, 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, errorSchema, data)
			_, err := parseResponse([]byte(data), sent(errorSchemaUUID), playgroundTrust())
			assert.ErrorContains(t, err, "reading response: error code")
		})
	}
}

// A confirmation without a POK is refused when it is read. Through
// ParseResponse, removing the POK from a signed reply fails the digest
// first, so the check is tested on the element itself.
func TestConfirmationNeedsPOK(t *testing.T) {
	c := new(Confirmation)
	err := xml.Unmarshal([]byte(`<Potvrzeni xmlns="http://fs.gov.cz/eet/schema/v4" test="true"/>`), c)
	assert.EqualError(t, err, "confirmation without a POK")

	err = xml.Unmarshal([]byte(`<Potvrzeni xmlns="http://fs.gov.cz/eet/schema/v4" pok="9765cced-b373-4a13-bbdc-986a62bc3544-ff" test="true"/>`), c)
	require.NoError(t, err)
	assert.Equal(t, &Confirmation{POK: "9765cced-b373-4a13-bbdc-986a62bc3544-ff", Test: true}, c)
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
			err: "server: soapenv:Server: Internal Error",
		},
		{
			name: "not XML",
			data: `<soapenv:Envelope`,
			err:  "reading message:",
		},
		{
			// SOAP 1.1, section 3: a SOAP message must not contain a
			// document type declaration. One could declare IDs.
			name: "document type declaration",
			data: `<!DOCTYPE soapenv:Envelope [<!ATTLIST eet:Odpoved x ID #IMPLIED>]>` +
				`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body/></soapenv:Envelope>`,
			err: "response must not declare a document type (SOAP 1.1, section 3)",
		},
		{
			name: "empty",
			data: "",
			err:  "reading message:",
		},
		{
			name: "HTML page",
			data: "<html><body>Service Unavailable</body></html>",
			err:  "response is not a SOAP envelope",
		},
		{
			name: "no answer",
			data: `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body/></soapenv:Envelope>`,
			err:  "response has no Odpoved",
		},
		{
			name: "neither confirmation nor error",
			data: `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:eet="http://fs.gov.cz/eet/schema/v4">` +
				`<soapenv:Body><eet:Odpoved><eet:Hlavicka uuid_zpravy="f1263599-5a52-495a-a9a9-4e1e3b3efeb4"/></eet:Odpoved></soapenv:Body></soapenv:Envelope>`,
			err: "response has neither a confirmation nor an error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseResponse([]byte(tt.data), sent(confirmationUUID), playgroundTrust())
			assert.ErrorContains(t, err, tt.err)
		})
	}
}

func TestErrorCodeRetry(t *testing.T) {
	retry := map[ErrorCode]bool{
		-999:                true,
		-2:                  true,
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
		assert.Equal(t, expected, code.Retryable(), code)
	}
}

// The embedded certificates are the ones whose fingerprints ca/README.md
// lists.
func TestEmbeddedCertificates(t *testing.T) {
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
	tr := defaultTrust(false)
	assert.NotNil(t, tr.roots)
	assert.NotNil(t, tr.intermediates)
	assert.False(t, tr.playground)
	assert.Nil(t, tr.now)
}

// UUIDs of the messages the fixtures answer.
const (
	confirmationUUID = "f1263599-5a52-495a-a9a9-4e1e3b3efeb4"
	errorSchemaUUID  = "6c21a587-2dcd-4c99-9755-6347f02ac60b"
	verificationUUID = "97e1a938-b18e-4adb-a5b8-2e1f6a08ce15"
)

// sent is a sale message as far as ParseResponse needs it: its UUID.
func sent(uuid string) *Sale {
	return &Sale{Header: &Header{UUID: uuid}}
}

// playgroundTrust accepts the Playground's confirmations, checking
// certificates on the day the fixtures were made so that the tests outlive
// the Playground certificate.
func playgroundTrust() *trust {
	tr := defaultTrust(false)
	tr.playground, tr.identity = true, playgroundIdentity
	tr.now = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }
	return tr
}

func readResponse(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("test/responses/" + name + ".xml")
	require.NoError(t, err)
	return data
}

func TestParseResponseUnreadable(t *testing.T) {
	_, err := ParseResponse([]byte("<html/>"), sent(errorSchemaUUID), EnvironmentPlayground)
	assert.ErrorIs(t, err, ErrConnection)
}

// Fault codes are qualified names, so the local part is compared
// case-sensitively.
func TestFaultCodeCase(t *testing.T) {
	assert.ErrorIs(t, faultError("soapenv:Server", "x", true), ErrServer)
	assert.ErrorIs(t, faultError("Server", "x", true), ErrServer)
	assert.ErrorIs(t, faultError("soapenv:server", "x", true), ErrValidation)
	assert.ErrorIs(t, faultError("soapenv:Server.Busy", "x", true), ErrServer)
	assert.ErrorIs(t, faultError("Server.Timeout", "x", true), ErrServer)
	assert.ErrorIs(t, faultError("soapenv:ServerBusy", "x", true), ErrValidation)
	assert.ErrorIs(t, faultError("soapenv:Client.Malformed", "x", true), ErrValidation)
	assert.ErrorIs(t, faultError("x:Server", "x", false), ErrValidation)

	// The prefix is resolved where the fault code is.
	for code, want := range map[string]*Error{
		`<faultcode>soapenv:Server</faultcode>`:                         ErrServer,
		`<faultcode xmlns:x="urn:other">x:Server</faultcode>`:           ErrValidation,
		`<faultcode xmlns:x="` + nsSOAP + `">x:Server.Busy</faultcode>`: ErrServer,
		`<faultcode>Server</faultcode>`:                                 ErrServer,
		"<faultcode>\n  soapenv:Server\n</faultcode>":                   ErrServer,
		"<faultcode> Server.Busy </faultcode>":                          ErrServer,
		"<faultcode xmlns:x=\"urn:other\">\n  x:Server\n</faultcode>":   ErrValidation,
	} {
		reply := `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault>` +
			code + `<faultstring>x</faultstring></soapenv:Fault></soapenv:Body></soapenv:Envelope>`
		_, err := parseResponse([]byte(reply), sent(errorSchemaUUID), playgroundTrust())
		assert.ErrorIs(t, err, want, code)
	}
}

// An error code comes back with the response and as an error, as Send gives
// it.
func TestParseResponseReturnsErrorCode(t *testing.T) {
	res, err := ParseResponse(readResponse(t, "error-schema"), sent(errorSchemaUUID), EnvironmentPlayground)
	assert.ErrorIs(t, err, ErrValidation.WithCode("3"))
	require.NotNil(t, res)
	assert.Equal(t, ErrorCodeSchema, res.Error.Code)
}
