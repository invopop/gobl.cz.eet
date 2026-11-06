package eet

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"

	"github.com/beevik/etree"
	"software.sslmate.com/src/go-pkcs12"
)

// Signer signs EET requests with the merchant's own certificate, the one the
// CA EET issues through DIS+ with the merchant's tax ID as CN.
type Signer struct {
	certificate *x509.Certificate
	key         crypto.Signer
}

// NewSigner prepares a signer from the merchant's certificate and its
// private key, which must be RSA: the service only takes RSA-SHA256.
func NewSigner(cert *x509.Certificate, key crypto.Signer) (*Signer, error) {
	public, ok := key.Public().(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("eet: private key must be RSA")
	}
	if !public.Equal(cert.PublicKey) {
		return nil, errors.New("eet: private key does not match the certificate")
	}
	return &Signer{certificate: cert, key: key}, nil
}

// NewSignerFromPKCS12 prepares a signer from a PKCS#12 (.p12) file as the
// CA EET issues it, including the legacy RC2 and 3DES encryption it uses.
func NewSignerFromPKCS12(data []byte, password string) (*Signer, error) {
	key, cert, _, err := pkcs12.DecodeChain(data, password)
	if err != nil {
		return nil, fmt.Errorf("eet: reading PKCS#12: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("eet: private key must be RSA")
	}
	return NewSigner(cert, signer)
}

// Certificate returns the certificate the signer signs with.
func (s *Signer) Certificate() *x509.Certificate {
	return s.certificate
}

// Sign returns the SOAP request that carries the sale, signed the way the
// EET service requires: only the body is signed, with exclusive
// canonicalization, SHA-256 and RSA-SHA256, and the certificate travels in
// a BinarySecurityToken. Element IDs derive from the message UUID.
func (s *Signer) Sign(sale *Sale) ([]byte, error) {
	if sale == nil || sale.Header == nil || sale.Data == nil {
		return nil, errors.New("eet: sale needs a header and data")
	}
	return s.sign(sale, sale.Header.UUID)
}

func (s *Signer) sign(sale *Sale, idSuffix string) ([]byte, error) {
	data, err := xml.Marshal(newEnvelope(sale, s.certificate, idSuffix))
	if err != nil {
		return nil, fmt.Errorf("eet: building request: %w", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, fmt.Errorf("eet: building request: %w", err)
	}

	body, err := canonicalize(doc.FindElement("/soapenv:Envelope/soapenv:Body"), bodyPrefixList)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	doc.FindElement("//ds:DigestValue").SetText(base64.StdEncoding.EncodeToString(digest[:]))

	signedInfo, err := canonicalize(doc.FindElement("//ds:SignedInfo"), signedInfoPrefixList)
	if err != nil {
		return nil, err
	}
	hashed := sha256.Sum256(signedInfo)
	value, err := s.key.Sign(rand.Reader, hashed[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("eet: signing: %w", err)
	}
	doc.FindElement("//ds:SignatureValue").SetText(base64.StdEncoding.EncodeToString(value))

	return doc.WriteToBytes()
}
