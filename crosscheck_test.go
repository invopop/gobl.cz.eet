package eet

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests below cross-check helium's verification with a second judge
// built on goxmldsig's exclusive canonicalization and crypto/rsa: both must
// accept the genuine messages, ours included, and refuse the tampered ones.

func TestSecondJudgeAcceptsGenuineMessages(t *testing.T) {
	files := []string{
		"test/responses/confirmation.xml",
		"test/responses/warning-unit.xml",
		"test/samples/CZ00000019.eet.v4.req.xml",
		"test/samples/CZ683555118.eet.v4.req.xml",
		"test/samples/CZ8551015704.eet.v4.req.xml",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(name)
			require.NoError(t, err)
			assert.NoError(t, verifyWithGoxmldsig(t, data))
			_, err = verifyBytes(t, data)
			assert.NoError(t, err)
		})
	}
	t.Run("our signed request", func(t *testing.T) {
		data, err := newTestSigner(t).sign(officialSamples[0].sale)
		require.NoError(t, err)
		assert.NoError(t, verifyWithGoxmldsig(t, data))
		_, err = verifyBytes(t, data)
		assert.NoError(t, err)
	})
}

func TestSecondJudgeRefusesTampering(t *testing.T) {
	for _, tt := range tamperings {
		if tt.profileOnly {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			doc := readSample(t, "CZ00000019")
			tt.tamper(doc)
			data, err := doc.WriteToBytes()
			require.NoError(t, err)
			assert.Error(t, verifyWithGoxmldsig(t, data))
			_, err = verifyBytes(t, data)
			assert.Error(t, err)
		})
	}
	t.Run("changed POK", func(t *testing.T) {
		data := strings.Replace(string(readResponse(t, "confirmation")), `pok="9765cced`, `pok="9765ccee`, 1)
		assert.Error(t, verifyWithGoxmldsig(t, []byte(data)))
		_, err := parseResponse([]byte(data), sent(confirmationUUID), playgroundTrust())
		assert.Error(t, err)
	})
}

// ParseResponse never panics, and a confirmation it accepts is always one
// the second judge accepts too: whatever the input, a POK only comes out of
// a signature that verifies.
func FuzzParseResponse(f *testing.F) {
	for _, name := range []string{"confirmation", "warning-unit", "verification", "error-schema", "error-signature", "error-tax-id"} {
		f.Add(readResponse(f, name))
	}
	// An empty signature value outside the security header, which a judge
	// that searches the whole document would take for the signature's.
	f.Add([]byte(strings.Replace(string(readResponse(f, "confirmation")), "<wsse:Security", "<ds:SignatureValue/><wsse:Security", 1)))
	// A second token with the referenced ID and another certificate.
	f.Add([]byte(withDuplicateToken(f, string(readResponse(f, "confirmation")), true)))
	// A second copy of each part the profile allows only once.
	for _, d := range duplicateParts {
		f.Add([]byte(duplicatePart(f, d.fixture, d.open, d.close)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := parseResponse(data, sent(confirmationUUID), playgroundTrust())
		if err != nil || res.Confirmation == nil {
			return
		}
		if verr := verifyWithGoxmldsig(t, data); verr != nil {
			t.Fatalf("confirmation %s accepted, but the second judge refuses it: %v", res.Confirmation.POK, verr)
		}
	})
}

// verifyWithGoxmldsig checks the body's digest and the RSA signature over
// SignedInfo with goxmldsig's canonicalization, reading every part from the
// header's security element as ParseResponse does.
func verifyWithGoxmldsig(t testing.TB, data []byte) error {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return err
	}
	parts, err := signatureParts(doc)
	if err != nil {
		return err
	}
	if parts.reference.SelectAttrValue("URI", "") != "#"+etreeWSUID(parts.body) {
		return errors.New("reference is not to the body")
	}
	canonicalBody, err := canonicalizeEtree(parts.body, parts.bodyPrefixes)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonicalBody)
	expected, err := decodeBase64(parts.digestValue.Text())
	if err != nil || !bytes.Equal(digest[:], expected) {
		return errors.New("digest does not match")
	}
	der, err := decodeBase64(parts.token.Text())
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	public, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("certificate key is not RSA")
	}
	canonicalSignedInfo, err := canonicalizeEtree(parts.signedInfo, parts.methodPrefixes)
	if err != nil {
		return err
	}
	value, err := decodeBase64(parts.signatureValue.Text())
	if err != nil {
		return err
	}
	hashed := sha256.Sum256(canonicalSignedInfo)
	return rsa.VerifyPKCS1v15(public, crypto.SHA256, hashed[:], value)
}

// etreeSignature holds the parts of the signature in the SOAP header's
// security element, and the body and token it refers to.
type etreeSignature struct {
	body, security, signature, signedInfo, method, reference *etree.Element
	transform, digestValue, signatureValue, token            *etree.Element

	// methodPrefixes and bodyPrefixes are the InclusiveNamespaces prefix
	// lists of SignedInfo's canonicalization and of the body's transform.
	methodPrefixes, bodyPrefixes string
}

// signatureParts finds each part of the signature on its own path, from the
// envelope down, never by searching the whole document, and refuses a part
// that appears twice, as ParseResponse does.
func signatureParts(doc *etree.Document) (*etreeSignature, error) {
	var err error
	one := func(parent *etree.Element, namespace, tag string) *etree.Element {
		if err != nil || parent == nil {
			return nil
		}
		var found *etree.Element
		for _, c := range parent.ChildElements() {
			if c.Tag != tag || c.NamespaceURI() != namespace {
				continue
			}
			if found != nil {
				err = fmt.Errorf("more than one %s", tag)
				return nil
			}
			found = c
		}
		return found
	}
	root := doc.Root()
	p := &etreeSignature{body: one(root, nsSOAP, "Body")}
	p.security = one(one(root, nsSOAP, "Header"), nsWSSE, "Security")
	p.signature = one(p.security, nsDSig, "Signature")
	p.signedInfo = one(p.signature, nsDSig, "SignedInfo")
	p.method = one(p.signedInfo, nsDSig, "CanonicalizationMethod")
	one(p.signedInfo, nsDSig, "SignatureMethod")
	p.reference = one(p.signedInfo, nsDSig, "Reference")
	p.transform = one(one(p.reference, nsDSig, "Transforms"), nsDSig, "Transform")
	p.methodPrefixes = etreePrefixes(one(p.method, nsExcC14N, "InclusiveNamespaces"))
	p.bodyPrefixes = etreePrefixes(one(p.transform, nsExcC14N, "InclusiveNamespaces"))
	one(p.reference, nsDSig, "DigestMethod")
	p.digestValue = one(p.reference, nsDSig, "DigestValue")
	p.signatureValue = one(p.signature, nsDSig, "SignatureValue")
	tokenRef := one(one(one(p.signature, nsDSig, "KeyInfo"), nsWSSE, "SecurityTokenReference"), nsWSSE, "Reference")
	if err != nil {
		return nil, err
	}
	if tokenRef != nil && p.security != nil {
		id := strings.TrimPrefix(tokenRef.SelectAttrValue("URI", ""), "#")
		for _, c := range p.security.ChildElements() {
			if c.Tag == "BinarySecurityToken" && c.NamespaceURI() == nsWSSE && id != "" && etreeWSUID(c) == id {
				if p.token != nil {
					return nil, errors.New("more than one token with the referenced ID")
				}
				p.token = c
			}
		}
	}
	for _, el := range []*etree.Element{p.body, p.signedInfo, p.reference, p.digestValue, p.signatureValue, p.token} {
		if el == nil {
			return nil, errors.New("missing element")
		}
	}
	return p, nil
}

// canonicalizeEtree returns the exclusive canonical form of el with goxmldsig,
// with the namespaces its ancestors declare in scope.
func canonicalizeEtree(el *etree.Element, prefixList string) ([]byte, error) {
	ctx, err := etreeutils.NSBuildParentContext(el)
	if err != nil {
		return nil, err
	}
	detached, err := etreeutils.NSDetatch(ctx, el)
	if err != nil {
		return nil, err
	}
	doc := etree.NewDocument()
	doc.SetRoot(detached)
	return dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList(prefixList).Canonicalize(doc.Root())
}

func etreePrefixList(method *etree.Element) string {
	if method == nil {
		return ""
	}
	for _, c := range method.ChildElements() {
		if c.Tag == "InclusiveNamespaces" && c.NamespaceURI() == nsExcC14N {
			return c.SelectAttrValue("PrefixList", "")
		}
	}
	return ""
}

// etreePrefixes is the PrefixList of an InclusiveNamespaces element, or
// empty without one.
func etreePrefixes(inclusive *etree.Element) string {
	if inclusive == nil {
		return ""
	}
	return inclusive.SelectAttrValue("PrefixList", "")
}

// etreeWSUID is the wsu:Id of an etree element.
func etreeWSUID(el *etree.Element) string {
	for _, attr := range el.Attr {
		if attr.Key == "Id" && attr.NamespaceURI() == nsWSU {
			return attr.Value
		}
	}
	return ""
}

// verifyBytes reads a message and checks its signature the way
// ParseResponse does.
func verifyBytes(t *testing.T, data []byte) (*x509.Certificate, error) {
	t.Helper()
	doc, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	cert, _, err := verifySignature(doc)
	return cert, err
}

// A token duplicated with the ID the KeyInfo points to, holding another
// certificate, is refused by both judges, whichever comes first.
func TestDuplicateSecurityTokenRefused(t *testing.T) {
	confirmation := string(readResponse(t, "confirmation"))
	for name, before := range map[string]bool{"before the original": true, "after the original": false} {
		t.Run(name, func(t *testing.T) {
			data := withDuplicateToken(t, confirmation, before)
			_, err := parseResponse([]byte(data), sent(confirmationUUID), playgroundTrust())
			assert.ErrorContains(t, err, "more than one BinarySecurityToken with the referenced ID")
			assert.Error(t, verifyWithGoxmldsig(t, []byte(data)))
		})
	}
}

// withDuplicateToken copies the reply's BinarySecurityToken, with the same
// wsu:Id and a throwaway certificate, before or after the original.
func withDuplicateToken(t testing.TB, reply string, before bool) string {
	t.Helper()
	start := strings.Index(reply, "<wsse:BinarySecurityToken")
	end := strings.Index(reply, "</wsse:BinarySecurityToken>") + len("</wsse:BinarySecurityToken>")
	require.True(t, start >= 0 && end > start)
	original := reply[start:end]
	open := original[:strings.Index(original, ">")+1]
	_, cert := newTestCertificate(t)
	duplicate := open + base64.StdEncoding.EncodeToString(cert.Raw) + "</wsse:BinarySecurityToken>"
	if before {
		return reply[:start] + duplicate + reply[start:]
	}
	return reply[:end] + duplicate + reply[end:]
}

// duplicateParts are the parts of a reply that may appear only once (interface
// description, 5.2, and the EET schema), each found by its tag in a fixture.
var duplicateParts = []struct {
	local, fixture, open, close string
	signed                      bool
	// err is ParseResponse's error, when not about a duplicate.
	err string
}{
	{"SignedInfo", "confirmation", "<SignedInfo>", "</SignedInfo>", true, ""},
	{"CanonicalizationMethod", "confirmation", "<CanonicalizationMethod ", "", true, ""},
	{"SignatureMethod", "confirmation", "<SignatureMethod ", "", true, ""},
	{"Transforms", "confirmation", "<Transforms>", "</Transforms>", true, ""},
	{"Transform", "confirmation", "<Transform ", "", true, "body must be transformed with exclusive canonicalization only"},
	{"DigestMethod", "confirmation", "<DigestMethod ", "", true, ""},
	{"DigestValue", "confirmation", "<DigestValue>", "</DigestValue>", true, ""},
	{"SignatureValue", "confirmation", "<SignatureValue>", "</SignatureValue>", true, ""},
	{"KeyInfo", "confirmation", "<KeyInfo>", "</KeyInfo>", true, ""},
	{"SecurityTokenReference", "confirmation", "<wsse:SecurityTokenReference", "</wsse:SecurityTokenReference>", true, ""},
	{"Reference", "confirmation", "<wsse:Reference ", "", true, ""},
	{"InclusiveNamespaces", "two-prefix-lists", "", "", true, ""},
	{"Fault", "fault", "<soapenv:Fault>", "</soapenv:Fault>", false, ""},
	{"faultcode", "fault", "<faultcode>", "</faultcode>", false, ""},
	{"faultstring", "fault", "<faultstring>", "</faultstring>", false, ""},
	{"Hlavicka", "confirmation", "<eet:Hlavicka ", "", false, ""},
	{"Potvrzeni", "confirmation", "<eet:Potvrzeni ", "", false, ""},
	{"Chyba", "error-schema", "<eet:Chyba ", "</eet:Chyba>", false, ""},
}

// A second copy of a part allowed only once is refused, by ParseResponse
// and, for the signature's parts, by the second judge.
func TestDuplicatePartsRefused(t *testing.T) {
	for _, d := range duplicateParts {
		t.Run(d.local, func(t *testing.T) {
			data := []byte(duplicatePart(t, d.fixture, d.open, d.close))
			uuid := confirmationUUID
			if d.fixture == "error-schema" {
				uuid = errorSchemaUUID
			}
			_, err := parseResponse(data, sent(uuid), playgroundTrust())
			want := "more than one " + d.local
			if d.err != "" {
				want = d.err
			}
			assert.ErrorContains(t, err, want)
			if d.signed {
				assert.ErrorContains(t, verifyWithGoxmldsig(t, data), "more than one "+d.local)
			}
		})
	}
}

// withDuplicate copies the first element that starts with open, up to end
// or, without one, to the end of its empty tag, right after itself.
func withDuplicate(t testing.TB, reply, open, end string) string {
	t.Helper()
	start := strings.Index(reply, open)
	require.GreaterOrEqual(t, start, 0, open)
	var stop int
	if end == "" {
		stop = start + strings.Index(reply[start:], "/>") + len("/>")
	} else {
		stop = start + strings.Index(reply[start:], end) + len(end)
	}
	require.Greater(t, stop, start, open)
	return reply[:stop] + reply[start:stop] + reply[stop:]
}

// serverFault is a SOAP fault as the service would send one.
const serverFault = `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault>` +
	`<faultcode>soapenv:Server</faultcode><faultstring>Internal Error</faultstring></soapenv:Fault></soapenv:Body></soapenv:Envelope>`

// duplicatePart is the fixture with the part duplicated: "fault" is
// serverFault, and, as the fixtures have no InclusiveNamespaces,
// "two-prefix-lists" gives the body's transform two of them.
func duplicatePart(t testing.TB, fixture, open, end string) string {
	t.Helper()
	switch fixture {
	case "fault":
		return withDuplicate(t, serverFault, open, end)
	case "two-prefix-lists":
	default:
		return withDuplicate(t, string(readResponse(t, fixture)), open, end)
	}
	reply := string(readResponse(t, "confirmation"))
	transform := `<Transform Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"/>`
	require.Contains(t, reply, transform)
	inclusive := `<ec:InclusiveNamespaces xmlns:ec="http://www.w3.org/2001/10/xml-exc-c14n#" PrefixList="eet"/>`
	return strings.Replace(reply, transform, strings.TrimSuffix(transform, "/>")+">"+inclusive+inclusive+"</Transform>", 1)
}
