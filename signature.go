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
	"strings"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

// Namespaces of the SOAP envelope and its WS-Security header.
const (
	nsSOAP = "http://schemas.xmlsoap.org/soap/envelope/"
	nsWSSE = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	nsWSU  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	nsDSig = "http://www.w3.org/2000/09/xmldsig#"
)

// Algorithms the EET service uses, and the only ones accepted when checking
// a signature. The InclusiveNamespaces element of the exclusive
// canonicalization lives in the algorithm's own namespace.
const (
	algExcC14N   = "http://www.w3.org/2001/10/xml-exc-c14n#"
	algSHA256    = "http://www.w3.org/2001/04/xmlenc#sha256"
	algRSASHA256 = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"
	nsExcC14N    = algExcC14N
)

// Types of the certificate token.
const (
	tokenEncodingBase64 = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"
	tokenValueX509      = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-x509-token-profile-1.0#X509v3"
)

// InclusiveNamespaces prefix lists of the official sample requests, for the
// SignedInfo and for the body.
const (
	signedInfoPrefixList = "soapenv v4"
	bodyPrefixList       = "v4"
)

// verifySignature checks the WS-Security signature over the SOAP body of an
// EET message and returns the certificate that made it, for the caller to
// decide whether to trust it. Only what the service uses passes: one
// reference to the body, exclusive canonicalization, SHA-256 and
// RSA-SHA256, and the certificate in a BinarySecurityToken.
func verifySignature(doc *etree.Document) (*x509.Certificate, error) {
	body := child(doc.Root(), nsSOAP, "Body")
	if body == nil {
		return nil, errors.New("eet: message has no SOAP body")
	}
	security := child(child(doc.Root(), nsSOAP, "Header"), nsWSSE, "Security")
	signature := child(security, nsDSig, "Signature")
	if signature == nil {
		return nil, errors.New("eet: message has no signature")
	}
	signedInfo := child(signature, nsDSig, "SignedInfo")
	method := child(signedInfo, nsDSig, "CanonicalizationMethod")
	if alg := algorithm(method); alg != algExcC14N {
		return nil, fmt.Errorf("eet: unsupported canonicalization method %s", alg)
	}
	if alg := algorithm(child(signedInfo, nsDSig, "SignatureMethod")); alg != algRSASHA256 {
		return nil, fmt.Errorf("eet: unsupported signature method %s", alg)
	}

	reference := child(signedInfo, nsDSig, "Reference")
	if id := wsuID(body); id == "" || attrValue(reference, "URI") != "#"+id {
		return nil, errors.New("eet: signature must reference the SOAP body")
	}
	transforms := children(child(reference, nsDSig, "Transforms"))
	if len(transforms) != 1 || algorithm(transforms[0]) != algExcC14N {
		return nil, errors.New("eet: body must be transformed with exclusive canonicalization only")
	}
	if alg := algorithm(child(reference, nsDSig, "DigestMethod")); alg != algSHA256 {
		return nil, fmt.Errorf("eet: unsupported digest method %s", alg)
	}
	canonicalBody, err := canonicalize(body, prefixList(transforms[0]))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonicalBody)
	expected, err := decodeBase64(text(child(reference, nsDSig, "DigestValue")))
	if err != nil || !bytes.Equal(digest[:], expected) {
		return nil, errors.New("eet: digest does not match the SOAP body")
	}

	cert, err := tokenCertificate(security, signature)
	if err != nil {
		return nil, err
	}
	public, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("eet: signing certificate must have an RSA key")
	}
	canonicalSignedInfo, err := canonicalize(signedInfo, prefixList(method))
	if err != nil {
		return nil, err
	}
	value, err := decodeBase64(text(child(signature, nsDSig, "SignatureValue")))
	if err != nil {
		return nil, fmt.Errorf("eet: reading signature value: %w", err)
	}
	hashed := sha256.Sum256(canonicalSignedInfo)
	if err := rsa.VerifyPKCS1v15(public, crypto.SHA256, hashed[:], value); err != nil {
		return nil, fmt.Errorf("eet: invalid signature: %w", err)
	}
	return cert, nil
}

// tokenCertificate reads the certificate from the BinarySecurityToken that
// the signature's KeyInfo points to.
func tokenCertificate(security, signature *etree.Element) (*x509.Certificate, error) {
	tokenRef := child(child(child(signature, nsDSig, "KeyInfo"), nsWSSE, "SecurityTokenReference"), nsWSSE, "Reference")
	id := strings.TrimPrefix(attrValue(tokenRef, "URI"), "#")
	for _, token := range children(security) {
		if token.Tag != "BinarySecurityToken" || token.NamespaceURI() != nsWSSE || id == "" || wsuID(token) != id {
			continue
		}
		if token.SelectAttrValue("ValueType", "") != tokenValueX509 {
			return nil, errors.New("eet: security token must be an X.509 certificate")
		}
		der, err := decodeBase64(token.Text())
		if err != nil {
			return nil, fmt.Errorf("eet: reading security token: %w", err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("eet: reading security token: %w", err)
		}
		return cert, nil
	}
	return nil, errors.New("eet: signing certificate not found")
}

// canonicalize returns the exclusive canonical form of el, with the
// namespaces its ancestors declare in scope and the given InclusiveNamespaces
// prefix list.
func canonicalize(el *etree.Element, prefixList string) ([]byte, error) {
	doc, err := detach(el)
	if err != nil {
		return nil, err
	}
	return dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList(prefixList).Canonicalize(doc.Root())
}

// detach copies el into a document of its own, declaring on it the
// namespaces its ancestors put in scope.
func detach(el *etree.Element) (*etree.Document, error) {
	ctx, err := etreeutils.NSBuildParentContext(el)
	if err != nil {
		return nil, fmt.Errorf("eet: reading %s: %w", el.Tag, err)
	}
	detached, err := etreeutils.NSDetatch(ctx, el)
	if err != nil {
		return nil, fmt.Errorf("eet: reading %s: %w", el.Tag, err)
	}
	doc := etree.NewDocument()
	doc.SetRoot(detached)
	return doc, nil
}

// The helpers below take a nil element as an empty one, so reading a message
// that lacks an element gives a clear error further on instead of a panic.

// child returns the first child element of el with the namespace and local
// name given, or nil.
func child(el *etree.Element, namespace, tag string) *etree.Element {
	for _, c := range children(el) {
		if c.Tag == tag && c.NamespaceURI() == namespace {
			return c
		}
	}
	return nil
}

func children(el *etree.Element) []*etree.Element {
	if el == nil {
		return nil
	}
	return el.ChildElements()
}

func attrValue(el *etree.Element, key string) string {
	if el == nil {
		return ""
	}
	return el.SelectAttrValue(key, "")
}

func text(el *etree.Element) string {
	if el == nil {
		return ""
	}
	return el.Text()
}

func algorithm(el *etree.Element) string {
	return attrValue(el, "Algorithm")
}

func prefixList(method *etree.Element) string {
	return attrValue(child(method, nsExcC14N, "InclusiveNamespaces"), "PrefixList")
}

func wsuID(el *etree.Element) string {
	for _, attr := range el.Attr {
		if attr.Key == "Id" && attr.NamespaceURI() == nsWSU {
			return attr.Value
		}
	}
	return ""
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
}
