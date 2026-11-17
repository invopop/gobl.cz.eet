package eet

import (
	"context"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium/enum"
	"github.com/lestrrat-go/helium/xmldsig1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests below cross-check our signature checks with helium's XML
// signature verifier, an independent implementation. helium is handed the
// certificate from the BinarySecurityToken and the body to cover, so it
// judges the signature itself; the WS-Security profile around it, where the
// certificate travels and which elements may appear, is ours alone.

func TestHeliumAcceptsGenuineMessages(t *testing.T) {
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
			assert.NoError(t, verifyWithHelium(t, data))

			doc := etree.NewDocument()
			require.NoError(t, doc.ReadFromBytes(data))
			_, _, err = verifySignature(doc)
			assert.NoError(t, err)
		})
	}
}

func TestHeliumRefusesTampering(t *testing.T) {
	for _, tt := range tamperings {
		if tt.profileOnly {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			doc := readSample(t, "CZ00000019")
			tt.tamper(doc)
			data, err := doc.WriteToBytes()
			require.NoError(t, err)
			assert.Error(t, verifyWithHelium(t, data))
		})
	}
	t.Run("changed POK", func(t *testing.T) {
		data := strings.Replace(string(readResponse(t, "confirmation")), `pok="9765cced`, `pok="9765ccee`, 1)
		assert.Error(t, verifyWithHelium(t, []byte(data)))
		_, err := ParseResponse([]byte(data), sent(confirmationUUID), playgroundTrust())
		assert.Error(t, err)
	})
}

// verifyWithHelium checks the message's signature with helium, after marking
// the body's wsu:Id as an ID so the reference resolves, and requires the
// signature to cover the body.
func verifyWithHelium(t *testing.T, data []byte) error {
	t.Helper()
	ctx := context.Background()
	doc, err := helium.NewParser().Parse(ctx, data)
	require.NoError(t, err)
	root := doc.DocumentElement()
	body := heliumChild(root, nsSOAP, "Body")
	security := heliumChild(heliumChild(root, nsSOAP, "Header"), nsWSSE, "Security")
	signature := heliumChild(security, nsDSig, "Signature")
	token := heliumChild(security, nsWSSE, "BinarySecurityToken")
	require.NotNil(t, signature)
	require.NotNil(t, token)
	der, err := decodeBase64(string(token.Content()))
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	if body != nil {
		if id := body.GetAttributeNodeNS("Id", nsWSU); id != nil {
			id.SetAType(enum.AttrID)
		}
	}
	res, err := xmldsig1.NewVerifier(xmldsig1.X509CertKeySource(cert)).VerifyElement(ctx, doc, signature)
	if err != nil {
		return err
	}
	if !res.Covers(body) {
		return errors.New("signature does not cover the body")
	}
	return nil
}

func heliumChild(n helium.Node, namespace, local string) *helium.Element {
	if n == nil {
		return nil
	}
	for c := range helium.Children(n) {
		if el, ok := c.(*helium.Element); ok && el.LocalName() == local && el.URI() == namespace {
			return el
		}
	}
	return nil
}
