package eet

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/russellhaering/goxmldsig/etreeutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"software.sslmate.com/src/go-pkcs12"
)

// officialSamples are the three signed requests published with the EET 2.0
// documentation, signed with the Playground test certificates, and the sales
// they carry.
var officialSamples = []struct {
	name string
	sale *Sale
}{
	{
		name: "CZ00000019",
		sale: &Sale{
			Header: &Header{
				UUID:         "03965780-6457-4842-bd80-5f9195c0b8c8",
				SentAt:       "2026-07-01T09:02:18Z",
				FirstAttempt: true,
				Verification: ptr(false),
			},
			Data: &Data{
				TaxID:       "CZ00000019",
				UnitID:      "303",
				DeviceID:    "/5604/MA65",
				Number:      "00/2224/SO57",
				SoldAt:      "2026-07-01T09:02:18Z",
				Total:       "188580.00",
				PrepaidLoad: "25.00",
				PrepaidUse:  "302.00",
			},
		},
	},
	{
		name: "CZ683555118",
		sale: &Sale{
			Header: &Header{
				UUID:         "b16b955b-5913-4edc-b3cc-51e058c70684",
				SentAt:       "2026-07-01T09:00:10Z",
				FirstAttempt: true,
				Verification: ptr(false),
			},
			Data: &Data{
				TaxID:           "CZ683555118",
				DelegatingTaxID: "CZ00000019",
				DelegatedByMany: true,
				UnitID:          "101",
				DeviceID:        "/6304/WN90",
				Number:          "00/3115/HO47",
				SoldAt:          "2026-07-01T09:00:10Z",
				Total:           "32277.00",
				PrepaidLoad:     "25.00",
				PrepaidUse:      "302.00",
			},
		},
	},
	{
		name: "CZ8551015704",
		sale: &Sale{
			Header: &Header{
				UUID:         "b16b955b-5913-4edc-b3cc-51e058c70684",
				SentAt:       "2026-07-01T09:01:15Z",
				FirstAttempt: true,
				Verification: ptr(false),
			},
			Data: &Data{
				TaxID:           "CZ8551015704",
				DelegatingTaxID: "CZ00000019",
				DelegatedByMany: true,
				UnitID:          "202",
				DeviceID:        "/6304/NA90",
				Number:          "00/3115/SO47",
				SoldAt:          "2026-07-01T09:01:13Z",
				Total:           "32277.00",
				PrepaidLoad:     "25.00",
				PrepaidUse:      "302.00",
			},
		},
	},
}

func TestVerifyOfficialSamples(t *testing.T) {
	for _, sample := range officialSamples {
		t.Run(sample.name, func(t *testing.T) {
			cert, err := verifySignature(readSample(t, sample.name))
			require.NoError(t, err)
			assert.Equal(t, sample.name, cert.Subject.CommonName)
		})
	}
}

// Signing the sale of an official sample with the sample's Body ID gives the
// sample's DigestValue, and the official SignatureValue verifies over the
// SignedInfo we build: our request canonicalizes byte for byte like the
// official one, so the same key would sign it identically.
func TestSignReproducesOfficialSamples(t *testing.T) {
	signer := newTestSigner(t)
	for _, sample := range officialSamples {
		t.Run(sample.name, func(t *testing.T) {
			official := readSample(t, sample.name)
			idSuffix := strings.TrimPrefix(wsuID(findElement(t, official, nsSOAP, "Body")), "id-")

			data, err := signer.sign(sample.sale, idSuffix)
			require.NoError(t, err)
			ours := etree.NewDocument()
			require.NoError(t, ours.ReadFromBytes(data))

			assert.Equal(t,
				findElement(t, official, nsDSig, "DigestValue").Text(),
				findElement(t, ours, nsDSig, "DigestValue").Text(),
			)

			signedInfo, err := canonicalize(findElement(t, ours, nsDSig, "SignedInfo"), signedInfoPrefixList)
			require.NoError(t, err)
			officialCert, err := verifySignature(official)
			require.NoError(t, err)
			officialValue, err := base64.StdEncoding.DecodeString(findElement(t, official, nsDSig, "SignatureValue").Text())
			require.NoError(t, err)
			digest := sha256.Sum256(signedInfo)
			assert.NoError(t, rsa.VerifyPKCS1v15(officialCert.PublicKey.(*rsa.PublicKey), crypto.SHA256, digest[:], officialValue))

			cert, err := verifySignature(ours)
			require.NoError(t, err)
			assert.Equal(t, signer.Certificate(), cert)
		})
	}
}

func TestSignUsesMessageUUIDForIDs(t *testing.T) {
	signer := newTestSigner(t)
	data, err := signer.Sign(officialSamples[0].sale)
	require.NoError(t, err)

	doc := etree.NewDocument()
	require.NoError(t, doc.ReadFromBytes(data))
	assert.Equal(t, "id-03965780-6457-4842-bd80-5f9195c0b8c8", wsuID(findElement(t, doc, nsSOAP, "Body")))
	assert.Equal(t, "X509-03965780-6457-4842-bd80-5f9195c0b8c8", wsuID(findElement(t, doc, nsWSSE, "BinarySecurityToken")))
	assert.Equal(t, "#X509-03965780-6457-4842-bd80-5f9195c0b8c8", findElement(t, doc, nsWSSE, "Reference").SelectAttrValue("URI", ""))
}

func TestSignNeedsHeaderAndData(t *testing.T) {
	signer := newTestSigner(t)
	_, err := signer.Sign(&Sale{Header: &Header{UUID: "03965780-6457-4842-bd80-5f9195c0b8c8"}})
	assert.EqualError(t, err, "eet: sale needs a header and data")
	_, err = signer.Sign(&Sale{Data: &Data{}})
	assert.EqualError(t, err, "eet: sale needs a header and data")
}

func TestSignNeedsMessageUUID(t *testing.T) {
	signer := newTestSigner(t)
	_, err := signer.Sign(&Sale{Header: &Header{}, Data: &Data{}})
	assert.EqualError(t, err, "eet: sale needs a message UUID")
}

func TestVerifyRejectsTampering(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(doc *etree.Document)
		err    string
	}{
		{
			name: "body",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, NamespaceEET, "Data").CreateAttr("celk_trzba", "188580.01")
			},
			err: "eet: digest does not match the SOAP body",
		},
		{
			name: "digest value",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsDSig, "DigestValue").SetText("1O9rNvq2RoP0UbJuNUcEDaPvwvnH3HKRp7aVc2lb6r0=")
			},
			err: "eet: digest does not match the SOAP body",
		},
		{
			name: "signature value",
			tamper: func(doc *etree.Document) {
				value := findElementIn(doc, nsDSig, "SignatureValue")
				value.SetText("A" + value.Text()[1:])
			},
			err: "eet: invalid signature: crypto/rsa: verification error",
		},
		{
			name: "reference to another element",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsDSig, "Reference").CreateAttr("URI", "#X509-73E2DBECC9F62C5AB8177998819768121")
			},
			err: "eet: signature must reference the SOAP body",
		},
		{
			name: "signature method",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsDSig, "SignatureMethod").CreateAttr("Algorithm", "http://www.w3.org/2000/09/xmldsig#rsa-sha1")
			},
			err: "eet: unsupported signature method http://www.w3.org/2000/09/xmldsig#rsa-sha1",
		},
		{
			name: "digest method",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsDSig, "DigestMethod").CreateAttr("Algorithm", "http://www.w3.org/2000/09/xmldsig#sha1")
			},
			err: "eet: unsupported digest method http://www.w3.org/2000/09/xmldsig#sha1",
		},
		{
			name: "canonicalization method",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsDSig, "CanonicalizationMethod").CreateAttr("Algorithm", "http://www.w3.org/TR/2001/REC-xml-c14n-20010315")
			},
			err: "eet: unsupported canonicalization method http://www.w3.org/TR/2001/REC-xml-c14n-20010315",
		},
		{
			name: "missing signature",
			tamper: func(doc *etree.Document) {
				security := findElementIn(doc, nsWSSE, "Security")
				security.RemoveChild(findElementIn(doc, nsDSig, "Signature"))
			},
			err: "eet: message has no signature",
		},
		{
			name: "missing body",
			tamper: func(doc *etree.Document) {
				doc.Root().RemoveChild(findElementIn(doc, nsSOAP, "Body"))
			},
			err: "eet: message has no SOAP body",
		},
		{
			name: "missing reference",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsDSig, "SignedInfo").RemoveChild(findElementIn(doc, nsDSig, "Reference"))
			},
			err: "eet: signature must reference the SOAP body",
		},
		{
			name: "extra transform",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsDSig, "Transforms").CreateElement("ds:Transform").CreateAttr("Algorithm", "http://www.w3.org/2000/09/xmldsig#enveloped-signature")
			},
			err: "eet: body must be transformed with exclusive canonicalization only",
		},
		{
			name: "token reference to nothing",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsWSSE, "Reference").CreateAttr("URI", "#X509-unknown")
			},
			err: "eet: signing certificate not found",
		},
		{
			name: "token that isn't a certificate",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsWSSE, "BinarySecurityToken").CreateAttr("ValueType", "http://docs.oasis-open.org/wss/oasis-wss-kerberos-token-profile-1.1#GSS_Kerberosv5_AP_REQ")
			},
			err: "eet: security token must be an X.509 certificate",
		},
		{
			name: "token that isn't base64",
			tamper: func(doc *etree.Document) {
				findElementIn(doc, nsWSSE, "BinarySecurityToken").SetText("!!!")
			},
			err: "eet: reading security token: illegal base64 data at input byte 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := readSample(t, "CZ00000019")
			tt.tamper(doc)
			_, err := verifySignature(doc)
			assert.EqualError(t, err, tt.err)
		})
	}
}

func TestNewSigner(t *testing.T) {
	t.Run("RSA key matching the certificate", func(t *testing.T) {
		key, cert := newTestCertificate(t)
		signer, err := NewSigner(cert, key)
		require.NoError(t, err)
		assert.Equal(t, cert, signer.Certificate())
	})
	t.Run("key from another certificate", func(t *testing.T) {
		_, cert := newTestCertificate(t)
		other, _ := newTestCertificate(t)
		_, err := NewSigner(cert, other)
		assert.EqualError(t, err, "eet: private key does not match the certificate")
	})
	t.Run("non-RSA key", func(t *testing.T) {
		_, cert := newTestCertificate(t)
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		_, err = NewSigner(cert, key)
		assert.EqualError(t, err, "eet: private key must be RSA")
	})
}

// The CA EET issues PKCS#12 files with the legacy encryption (RC2-40 or
// 3DES) that OpenSSL 3 only opens with -legacy.
func TestNewSignerFromPKCS12(t *testing.T) {
	key, cert := newTestCertificate(t)
	encoders := map[string]*pkcs12.Encoder{
		"legacy RC2": pkcs12.LegacyRC2,
		"legacy DES": pkcs12.LegacyDES,
		"modern":     pkcs12.Modern,
	}
	for name, encoder := range encoders {
		t.Run(name, func(t *testing.T) {
			data, err := encoder.Encode(key, cert, nil, "k8Lp2qZx")
			require.NoError(t, err)
			signer, err := NewSignerFromPKCS12(data, "k8Lp2qZx")
			require.NoError(t, err)
			assert.Equal(t, cert.Raw, signer.Certificate().Raw)
		})
	}
	t.Run("wrong password", func(t *testing.T) {
		data, err := pkcs12.LegacyRC2.Encode(key, cert, nil, "k8Lp2qZx")
		require.NoError(t, err)
		_, err = NewSignerFromPKCS12(data, "wrong")
		assert.ErrorContains(t, err, "eet: reading PKCS#12")
	})
}

func ptr[T any](v T) *T {
	return &v
}

func readSample(t *testing.T, name string) *etree.Document {
	t.Helper()
	data, err := os.ReadFile("test/samples/" + name + ".eet.v4.req.xml")
	require.NoError(t, err)
	doc := etree.NewDocument()
	require.NoError(t, doc.ReadFromBytes(data))
	return doc
}

func findElement(t *testing.T, doc *etree.Document, namespace, tag string) *etree.Element {
	t.Helper()
	el := findElementIn(doc, namespace, tag)
	require.NotNil(t, el, "no %s element", tag)
	return el
}

func findElementIn(doc *etree.Document, namespace, tag string) *etree.Element {
	el, err := etreeutils.NSFindOne(doc.Root(), namespace, tag)
	if err != nil {
		return nil
	}
	return el
}

func newTestSigner(t *testing.T) *Signer {
	t.Helper()
	key, cert := newTestCertificate(t)
	signer, err := NewSigner(cert, key)
	require.NoError(t, err)
	return signer
}

// newTestCertificate makes a throwaway self-signed certificate shaped like
// the ones the CA EET issues: RSA 2048 and the merchant's tax ID as CN.
func newTestCertificate(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "CZ00000019", Country: []string{"CZ"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(366 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, cert
}
