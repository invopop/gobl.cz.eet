package eet

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// standIn is a chain shaped like the one that signs EET's confirmations: a
// root, an intermediate, and the certificates the intermediate issues.
type standIn struct {
	t         *testing.T
	root      *x509.Certificate
	issuer    *x509.Certificate
	issuerKey *rsa.PrivateKey
	serial    int64
}

func newStandIn(t *testing.T) *standIn {
	t.Helper()
	rootKey := newKey(t)
	root := newCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Stand-in Root CA"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil, &rootKey.PublicKey, rootKey)
	issuerKey := newKey(t)
	issuer := newCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Stand-in Public CA"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, root, &issuerKey.PublicKey, rootKey)
	return &standIn{t: t, root: root, issuer: issuer, issuerKey: issuerKey, serial: 2}
}

// trust accepts the stand-in chain the way production accepts I.CA's,
// with the published signers.
func (s *standIn) trust() *trust {
	roots := x509.NewCertPool()
	roots.AddCert(s.root)
	intermediates := x509.NewCertPool()
	intermediates.AddCert(s.issuer)
	return &trust{identity: productionIdentity, roots: roots, intermediates: intermediates}
}

// issue makes a certificate with the identity given, changed by the
// function when there is one.
func (s *standIn) issue(id identity, change func(*x509.Certificate)) (*rsa.PrivateKey, *x509.Certificate) {
	s.t.Helper()
	s.serial++
	template := &x509.Certificate{
		SerialNumber: big.NewInt(s.serial),
		Subject: pkix.Name{
			CommonName:   id.commonName,
			SerialNumber: id.serialNumber,
			Organization: []string{id.organization},
			Country:      []string{id.country},
			ExtraNames:   []pkix.AttributeTypeAndValue{{Type: oidOrganizationIdentifier, Value: id.organizationID}},
		},
		Policies:    []x509.OID{policyOID(s.t, id.policy)},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection, x509.ExtKeyUsageClientAuth},
	}
	if change != nil {
		change(template)
	}
	key := newKey(s.t)
	return key, newCertificate(s.t, template, s.issuer, &key.PublicKey, s.issuerKey)
}

func policyOID(t *testing.T, oid asn1.ObjectIdentifier) x509.OID {
	t.Helper()
	ints := make([]uint64, len(oid))
	for i, n := range oid {
		ints[i] = uint64(n)
	}
	policy, err := x509.OIDFromInts(ints)
	require.NoError(t, err)
	return policy
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func newCertificate(t *testing.T, template, parent *x509.Certificate, public *rsa.PublicKey, signer *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	template.NotBefore = time.Now().Add(-time.Hour)
	template.NotAfter = time.Now().Add(24 * time.Hour)
	if parent == nil {
		parent = template
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, public, signer)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// forgeConfirmation takes the Playground's confirmation, sets its test
// attribute and signs it again with the key and certificate given, as
// someone holding them would.
func forgeConfirmation(t *testing.T, key *rsa.PrivateKey, cert *x509.Certificate, test string) []byte {
	t.Helper()
	return forge(t, key, cert, func(doc *etree.Document) {
		doc.FindElement("//Potvrzeni").CreateAttr("test", test)
	})
}

// forge changes the Playground's confirmation and signs it again with the
// key and certificate given.
func forge(t *testing.T, key *rsa.PrivateKey, cert *x509.Certificate, change func(doc *etree.Document)) []byte {
	t.Helper()
	doc := etree.NewDocument()
	require.NoError(t, doc.ReadFromBytes(readResponse(t, "confirmation")))

	change(doc)
	parts, err := signatureParts(doc)
	require.NoError(t, err)
	parts.token.SetText(base64.StdEncoding.EncodeToString(cert.Raw))

	canonicalBody, err := canonicalizeEtree(parts.body, parts.bodyPrefixes)
	require.NoError(t, err)
	digest := sha256.Sum256(canonicalBody)
	parts.digestValue.SetText(base64.StdEncoding.EncodeToString(digest[:]))

	canonicalSignedInfo, err := canonicalizeEtree(parts.signedInfo, parts.methodPrefixes)
	require.NoError(t, err)
	hashed := sha256.Sum256(canonicalSignedInfo)
	value, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	require.NoError(t, err)
	parts.signatureValue.SetText(base64.StdEncoding.EncodeToString(value))

	data, err := doc.WriteToBytes()
	require.NoError(t, err)
	return data
}

// The forged confirmation is a sound one: signed with production's exact
// identity, it is read like the real thing.
func TestForgedConfirmationIsWellFormed(t *testing.T) {
	chain := newStandIn(t)
	key, cert := chain.issue(productionIdentity, nil)
	res, err := parseResponse(forgeConfirmation(t, key, cert, "false"), sent(confirmationUUID), chain.trust())
	require.NoError(t, err)
	assert.Equal(t, "9765cced-b373-4a13-bbdc-986a62bc3544-ff", res.Confirmation.POK)
	assert.False(t, res.Confirmation.Test)
}

// The real Playground confirmation is accepted in the Playground and, as
// its signer is not production's, refused in production.
func TestTrustPlaygroundSigner(t *testing.T) {
	_, err := parseResponse(readResponse(t, "confirmation"), sent(confirmationUUID), playgroundTrust())
	assert.NoError(t, err)

	production := playgroundTrust()
	production.playground, production.identity = false, productionIdentity
	_, err = parseResponse(readResponse(t, "confirmation"), sent(confirmationUUID), production)
	assert.ErrorIs(t, err, ErrUntrusted)
	assert.EqualError(t, err, `untrusted: response signed by a certificate with common name "NEOSTRE - playground prostredi EET", expected "Spolecne technicke zarizeni spravce dane EET"`)
}

// A production confirmation is refused when any one attribute of its
// signer differs from the published identity, and when the signer is
// someone else's certificate under the same chain.
func TestTrustRefusesOtherSigners(t *testing.T) {
	chain := newStandIn(t)
	with := func(change func(id *identity)) identity {
		id := productionIdentity
		change(&id)
		return id
	}
	tests := []struct {
		name   string
		signer identity
		change func(c *x509.Certificate)
		err    string
	}{
		{
			name:   "another common name",
			signer: with(func(id *identity) { id.commonName = "Spolecne technicke zarizeni spravce dane" }),
			err:    `common name "Spolecne technicke zarizeni spravce dane", expected "Spolecne technicke zarizeni spravce dane EET"`,
		},
		{
			name:   "another serial number",
			signer: with(func(id *identity) { id.serialNumber = "ICA - 1275853" }),
			err:    `serial number "ICA - 1275853", expected "ICA - 1275857"`,
		},
		{
			name:   "no serial number",
			signer: with(func(id *identity) { id.serialNumber = "" }),
			err:    `serial number "", expected "ICA - 1275857"`,
		},
		{
			name:   "another organization identifier",
			signer: with(func(id *identity) { id.organizationID = "NTRCZ-26439395" }),
			err:    `organization identifier "NTRCZ-26439395", expected "NTRCZ-72080043"`,
		},
		{
			name:   "another organization",
			signer: with(func(id *identity) { id.organization = "První certifikační autorita, a.s." }),
			err:    `organization "[První certifikační autorita, a.s.]", expected "Generální finanční ředitelství"`,
		},
		{
			name:   "another country",
			signer: with(func(id *identity) { id.country = "SK" }),
			err:    `country "[SK]", expected "CZ"`,
		},
		{
			name:   "another policy",
			signer: with(func(id *identity) { id.policy = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 23624, 10, 1, 71, 1, 2} }),
			err:    `policies "[1.3.6.1.4.1.23624.10.1.71.1.2]", expected "1.3.6.1.4.1.23624.10.1.71.1.1"`,
		},
		{
			name:   "a second common name, the right one last",
			signer: productionIdentity,
			change: func(c *x509.Certificate) {
				c.Subject.ExtraNames = append(c.Subject.ExtraNames,
					pkix.AttributeTypeAndValue{Type: oidCommonName, Value: "Somebody else"},
					pkix.AttributeTypeAndValue{Type: oidCommonName, Value: productionIdentity.commonName})
			},
			err: `common name "Somebody else, Spolecne technicke zarizeni spravce dane EET", expected "Spolecne technicke zarizeni spravce dane EET"`,
		},
		{
			name:   "a second organization identifier, the right one last",
			signer: productionIdentity,
			change: func(c *x509.Certificate) {
				c.Subject.ExtraNames = []pkix.AttributeTypeAndValue{
					{Type: oidOrganizationIdentifier, Value: "NTRCZ-00000000"},
					{Type: oidOrganizationIdentifier, Value: productionIdentity.organizationID},
				}
			},
			err: `organization identifier "NTRCZ-00000000, NTRCZ-72080043", expected "NTRCZ-72080043"`,
		},
		{
			name:   "a second organization",
			signer: productionIdentity,
			change: func(c *x509.Certificate) { c.Subject.Organization = append(c.Subject.Organization, "Jiná s.r.o.") },
			err:    `organization "[Jiná s.r.o. Generální finanční ředitelství]", expected "Generální finanční ředitelství"`,
		},
		{
			// Other certificates of the organization under the same chain:
			// an e-mail certificate, a login certificate, and the
			// Playground's identity signing a production POK.
			name:   "e-mail certificate of the organization",
			signer: with(func(id *identity) { id.commonName, id.serialNumber = "Jan Novak", "ICA - 9000001" }),
			change: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection} },
			err:    `common name "Jan Novak", expected "Spolecne technicke zarizeni spravce dane EET"`,
		},
		{
			name:   "login certificate of the organization",
			signer: with(func(id *identity) { id.commonName, id.serialNumber = "Jan Novak", "ICA - 9000001" }),
			change: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} },
			err:    `common name "Jan Novak", expected "Spolecne technicke zarizeni spravce dane EET"`,
		},
		{
			name:   "Playground's identity, saying production",
			signer: playgroundIdentity,
			err:    `common name "NEOSTRE - playground prostredi EET", expected "Spolecne technicke zarizeni spravce dane EET"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, cert := chain.issue(tt.signer, tt.change)
			_, err := parseResponse(forgeConfirmation(t, key, cert, "false"), sent(confirmationUUID), chain.trust())
			assert.ErrorIs(t, err, ErrUntrusted)
			assert.EqualError(t, err, "untrusted: response signed by a certificate with "+tt.err)
		})
	}
}

func TestTrustEnvironments(t *testing.T) {
	chain := newStandIn(t)
	productionKey, production := chain.issue(productionIdentity, nil)
	playgroundKey, playground := chain.issue(playgroundIdentity, nil)
	playgroundTrust := chain.trust()
	playgroundTrust.playground, playgroundTrust.identity = true, playgroundIdentity

	t.Run("production's signer, saying Playground", func(t *testing.T) {
		// The signer is right: the signed test attribute still counts.
		_, err := parseResponse(forgeConfirmation(t, productionKey, production, "true"), sent(confirmationUUID), chain.trust())
		assert.ErrorIs(t, err, ErrEnvironment)
		assert.EqualError(t, err, "environment: confirmation from the Playground, expected production")
	})
	t.Run("production's signer in the Playground", func(t *testing.T) {
		_, err := parseResponse(forgeConfirmation(t, productionKey, production, "true"), sent(confirmationUUID), playgroundTrust)
		assert.ErrorIs(t, err, ErrUntrusted)
	})
	t.Run("Playground's signer in the Playground, without a serial number", func(t *testing.T) {
		_, err := parseResponse(forgeConfirmation(t, playgroundKey, playground, "true"), sent(confirmationUUID), playgroundTrust)
		assert.NoError(t, err)
	})
	t.Run("Playground's signer with any serial number", func(t *testing.T) {
		id := playgroundIdentity
		id.serialNumber = "ICA - 1275853"
		key, cert := chain.issue(id, nil)
		_, err := parseResponse(forgeConfirmation(t, key, cert, "true"), sent(confirmationUUID), playgroundTrust)
		assert.NoError(t, err)
	})
	t.Run("certificate named for the Playground with another organization identifier", func(t *testing.T) {
		id := playgroundIdentity
		id.organizationID = "NTRCZ-26439395"
		key, cert := chain.issue(id, nil)
		_, err := parseResponse(forgeConfirmation(t, key, cert, "true"), sent(confirmationUUID), playgroundTrust)
		assert.ErrorIs(t, err, ErrUntrusted)
	})
	t.Run("no trust", func(t *testing.T) {
		_, err := parseResponse(forgeConfirmation(t, productionKey, production, "false"), sent(confirmationUUID), nil)
		assert.EqualError(t, err, "untrusted: no trusted roots to check the response against")
	})
	t.Run("trust without roots", func(t *testing.T) {
		_, err := parseResponse(forgeConfirmation(t, productionKey, production, "false"), sent(confirmationUUID), &trust{})
		assert.ErrorIs(t, err, ErrUntrusted)
	})
}

// The published identities are those of the documents, and the real
// Playground signer has the Playground's.
func TestPublishedSigners(t *testing.T) {
	assert.Equal(t, "1.3.6.1.4.1.23624.10.1.71.1.1", productionIdentity.policy.String())
	assert.Equal(t, productionIdentity.policy, playgroundIdentity.policy)
	assert.Equal(t, "NTRCZ-72080043", productionIdentity.organizationID)
	assert.Empty(t, playgroundIdentity.serialNumber)

	doc := etree.NewDocument()
	require.NoError(t, doc.ReadFromBytes(readResponse(t, "confirmation")))
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(doc.FindElement("//BinarySecurityToken").Text()))
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	assert.NoError(t, playgroundIdentity.check(cert))
}

// Two cases no recorded reply covers, signed by a certificate the trust
// accepts, so that only the check named is left to refuse them.
func TestSignedConfirmationsRefused(t *testing.T) {
	chain := newStandIn(t)
	key, playground := chain.issue(playgroundIdentity, nil)
	tr := chain.trust()
	tr.playground, tr.identity = true, playgroundIdentity

	t.Run("production POK in the Playground", func(t *testing.T) {
		_, err := parseResponse(forgeConfirmation(t, key, playground, "false"), sent(confirmationUUID), tr)
		assert.ErrorIs(t, err, ErrEnvironment)
		assert.EqualError(t, err, "environment: confirmation from production, expected the Playground")
	})
	t.Run("production POK without the test attribute in the Playground", func(t *testing.T) {
		data := forge(t, key, playground, func(doc *etree.Document) {
			doc.FindElement("//Potvrzeni").RemoveAttr("test")
		})
		_, err := parseResponse(data, sent(confirmationUUID), tr)
		assert.ErrorIs(t, err, ErrEnvironment)
	})
	t.Run("confirmation without the message UUID", func(t *testing.T) {
		// The schema makes uuid_zpravy optional, but only an error may leave
		// it out: a POK must say which message it confirms.
		data := forge(t, key, playground, func(doc *etree.Document) {
			doc.FindElement("//Hlavicka").RemoveAttr("uuid_zpravy")
		})
		_, err := parseResponse(data, sent(confirmationUUID), tr)
		assert.ErrorIs(t, err, ErrConnection)
		assert.ErrorContains(t, err, "response answers another message")
	})
}

func TestEnvironmentURL(t *testing.T) {
	assert.Equal(t, "https://trzbyeet.gov.cz/eet/services/EETServiceSOAP/v4", EnvironmentProduction.URL())
	assert.Equal(t, "https://pg.trzbyeet.gov.cz/eet/services/EETServiceSOAP/v4", EnvironmentPlayground.URL())
	assert.Empty(t, Environment("sandbox").URL())
}

// Each environment refuses the other's confirmations.
func TestEnvironmentConfirmations(t *testing.T) {
	playground := readResponse(t, "confirmation")

	t.Run("Playground's confirmation in production", func(t *testing.T) {
		_, err := ParseResponse(playground, sent(confirmationUUID), EnvironmentProduction)
		assert.ErrorIs(t, err, ErrUntrusted)
	})
	t.Run("production's confirmation in the Playground", func(t *testing.T) {
		chain := newStandIn(t)
		key, cert := chain.issue(productionIdentity, nil)
		tr := chain.trust()
		_, err := parseResponse(forgeConfirmation(t, key, cert, "false"), sent(confirmationUUID), tr)
		require.NoError(t, err)

		tr.playground, tr.identity = true, playgroundIdentity
		_, err = parseResponse(forgeConfirmation(t, key, cert, "false"), sent(confirmationUUID), tr)
		assert.ErrorIs(t, err, ErrUntrusted)
	})
	t.Run("unknown environment", func(t *testing.T) {
		_, err := ParseResponse(playground, sent(confirmationUUID), Environment("sandbox"))
		assert.EqualError(t, err, `validation: unknown environment "sandbox"`)
	})
	t.Run("environments' checks", func(t *testing.T) {
		assert.False(t, EnvironmentProduction.trust().playground)
		assert.True(t, EnvironmentPlayground.trust().playground)
		assert.Nil(t, Environment("").trust())
	})
}

// A signed confirmation with a POK attribute in another namespace is
// refused, as the decoder would read it for the POK.
func TestSignedConfirmationWithNamespacedPOK(t *testing.T) {
	chain := newStandIn(t)
	key, cert := chain.issue(productionIdentity, nil)
	data := forge(t, key, cert, func(doc *etree.Document) {
		p := doc.FindElement("//Potvrzeni")
		p.CreateAttr("test", "false")
		p.CreateAttr("xmlns:x", "urn:x")
		p.CreateAttr("x:pok", "11111111-1111-4111-8111-111111111111-ff")
	})
	_, err := parseResponse(data, sent(confirmationUUID), chain.trust())
	assert.ErrorContains(t, err, "Potvrzeni has an attribute in another namespace")
}

// A signed reply with both a confirmation and an error is refused, as the
// schema's OdpovedType allows only one of them.
func TestSignedConfirmationWithError(t *testing.T) {
	chain := newStandIn(t)
	key, cert := chain.issue(productionIdentity, nil)
	data := forge(t, key, cert, func(doc *etree.Document) {
		p := doc.FindElement("//Potvrzeni")
		p.CreateAttr("test", "false")
		chyba := p.Parent().CreateElement(p.Space + ":Chyba")
		chyba.CreateAttr("kod", "4")
		chyba.SetText("Neplatny podpis SOAP zpravy")
	})
	_, err := parseResponse(data, sent(confirmationUUID), chain.trust())
	assert.EqualError(t, err, "response has both a confirmation and an error")
}

// A signed confirmation whose warning has a code attribute in another
// namespace is refused, as the decoder would read it for the code.
func TestSignedWarningWithNamespacedCode(t *testing.T) {
	chain := newStandIn(t)
	key, cert := chain.issue(productionIdentity, nil)
	data := forge(t, key, cert, func(doc *etree.Document) {
		p := doc.FindElement("//Potvrzeni")
		p.CreateAttr("test", "false")
		warning := p.Parent().CreateElement(p.Space + ":Varovani")
		warning.CreateAttr("xmlns:x", "urn:x")
		warning.CreateAttr("x:kod_varov", "1")
		warning.CreateAttr("kod_varov", "6")
		warning.SetText("Chybny format identifikatoru evidencni jednotky")
	})
	_, err := parseResponse(data, sent(confirmationUUID), chain.trust())
	assert.ErrorContains(t, err, "Varovani has an attribute in another namespace")
}
