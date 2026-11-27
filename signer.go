package eet

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"errors"
	"fmt"

	"github.com/invopop/xmldsig"
	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium/enum"
	"github.com/lestrrat-go/helium/xmldsig1"
)

// signer signs EET requests with the merchant's certificate and private key.
type signer struct {
	certificate *x509.Certificate
	key         crypto.Signer
}

// newSigner prepares a signer from the merchant's certificate and its
// private key, which must be RSA: the service only takes RSA-SHA256.
func newSigner(cert *x509.Certificate, key crypto.Signer) (*signer, error) {
	public, ok := key.Public().(*rsa.PublicKey)
	if !ok {
		return nil, ErrValidation.WithMessage("private key must be RSA")
	}
	if !public.Equal(cert.PublicKey) {
		return nil, ErrValidation.WithMessage("private key does not match the certificate")
	}
	return &signer{certificate: cert, key: key}, nil
}

// signerFor prepares a signer from a certificate as xmldsig loads it from
// the .p12 file the CA EET issues.
func signerFor(cert *xmldsig.Certificate) (*signer, error) {
	conf, err := tlsAuthConfig(cert)
	if err != nil {
		return nil, asError(err, ErrValidation.WithMessage("reading certificate: "+err.Error()))
	}
	pair := conf.Certificates[0]
	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, ErrValidation.WithMessage("private key must be RSA")
	}
	leaf := pair.Leaf
	if leaf == nil {
		// GODEBUG=x509keypairleaf=0 leaves it unparsed.
		if leaf, err = x509.ParseCertificate(pair.Certificate[0]); err != nil {
			return nil, ErrValidation.WithMessage("reading certificate: " + err.Error()).withCause(err)
		}
	}
	return newSigner(leaf, key)
}

// tlsAuthConfig reads the certificate's key and chain; xmldsig panics with
// a certificate that wasn't loaded.
func tlsAuthConfig(cert *xmldsig.Certificate) (conf *tls.Config, err error) {
	defer func() {
		if recover() != nil {
			conf, err = nil, ErrValidation.WithMessage("certificate is not loaded")
		}
	}()
	return cert.TLSAuthConfig()
}

// sign returns the signed SOAP request for the sale, with element IDs
// derived from its message UUID.
func (s *signer) sign(sale *Sale) ([]byte, error) {
	if sale == nil || sale.Header == nil || sale.Data == nil {
		return nil, ErrValidation.WithMessage("sale needs a header and data")
	}
	if sale.Header.UUID == "" {
		return nil, ErrValidation.WithMessage("sale needs a message UUID")
	}
	return s.signWithIDs(sale, sale.Header.UUID)
}

func (s *signer) signWithIDs(sale *Sale, idSuffix string) ([]byte, error) {
	ids := newSOAPEnvelopeIDs(idSuffix)
	data, err := xml.Marshal(newSOAPEnvelope(sale, s.certificate, ids))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	doc, err := parseXML(data)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if err := signBody(doc, ids, s.key); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := helium.NewWriter().XMLDeclaration(false).WriteTo(&out, doc); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}
	return out.Bytes(), nil
}

// signBody signs the SOAP body into the WS-Security header, in the profile
// of the interface description (5.2).
func signBody(doc *helium.Document, ids soapEnvelopeIDs, key crypto.Signer) error {
	root := doc.DocumentElement()
	body := child(root, nsSOAP, "Body")
	securityEl := child(child(root, nsSOAP, "Header"), nsWSSE, "Security")
	bodyID := body.GetAttributeNodeNS("Id", nsWSU)
	if securityEl == nil || bodyID == nil {
		return errors.New("message has no body or security header to sign")
	}
	// wsu:Id is an ID by the WS-Security schema, which helium doesn't read.
	bodyID.SetAType(enum.AttrID)

	dsig := xmldsig1.NewSigner().
		SignatureAlgorithm(algRSASHA256).
		CanonicalizationMethod(algExcC14N).
		SignatureID(ids.signature).
		KeyInfo(tokenKeyInfo{ids: ids}).
		Reference(xmldsig1.ReferenceConfig{
			URI:             "#" + ids.body,
			DigestAlgorithm: algSHA256,
			Transforms:      []xmldsig1.Transform{xmldsig1.ExcC14NTransform(bodyPrefixList)},
		})
	if err := dsig.SignEnveloped(context.Background(), doc, securityEl, key); err != nil {
		return fmt.Errorf("signing: %w", err)
	}
	return nil
}

// tokenKeyInfo points the signature's KeyInfo at the BinarySecurityToken, as
// the interface description asks (5.2).
type tokenKeyInfo struct {
	ids soapEnvelopeIDs
}

func (k tokenKeyInfo) BuildKeyInfo(_ context.Context, doc *helium.Document, _ any) (*helium.Element, error) {
	keyInfo, err := newElement(doc, "ds", nsDSig, "KeyInfo")
	if err != nil {
		return nil, err
	}
	if err := keyInfo.SetAttribute("Id", k.ids.keyInfo); err != nil {
		return nil, err
	}
	tokenRef, err := newElement(doc, "wsse", nsWSSE, "SecurityTokenReference")
	if err != nil {
		return nil, err
	}
	if err := tokenRef.DeclareNamespace("wsu", nsWSU); err != nil {
		return nil, err
	}
	if err := tokenRef.SetAttributeNS("Id", k.ids.tokenReference, helium.NewNamespace("wsu", nsWSU)); err != nil {
		return nil, err
	}
	reference, err := newElement(doc, "wsse", nsWSSE, "Reference")
	if err != nil {
		return nil, err
	}
	if err := reference.SetAttribute("URI", "#"+k.ids.token); err != nil {
		return nil, err
	}
	if err := reference.SetAttribute("ValueType", tokenValueX509); err != nil {
		return nil, err
	}
	if err := tokenRef.AddChild(reference); err != nil {
		return nil, err
	}
	if err := keyInfo.AddChild(tokenRef); err != nil {
		return nil, err
	}
	return keyInfo, nil
}

func newElement(doc *helium.Document, prefix, namespace, local string) (*helium.Element, error) {
	el, err := doc.CreateElement(local)
	if err != nil {
		return nil, err
	}
	if err := el.DeclareNamespace(prefix, namespace); err != nil {
		return nil, err
	}
	if err := el.SetActiveNamespace(prefix, namespace); err != nil {
		return nil, err
	}
	return el, nil
}
