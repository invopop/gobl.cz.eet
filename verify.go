package eet

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium/c14n"
	"github.com/lestrrat-go/helium/enum"
	"github.com/lestrrat-go/helium/xmldsig1"
)

// verifySignature checks the WS-Security signature over the SOAP body
// against the profile of the interface description (5.2), and returns the
// signing certificate, not yet trusted, and the canonical body the signature
// covers.
func verifySignature(doc *helium.Document) (*x509.Certificate, []byte, error) {
	root := doc.DocumentElement()
	body, err := onlyChild(root, nsSOAP, "Body")
	if err != nil {
		return nil, nil, err
	}
	if body == nil {
		return nil, nil, errors.New("message has no SOAP body")
	}
	header, err := onlyChild(root, nsSOAP, "Header")
	if err != nil {
		return nil, nil, err
	}
	securityEl, err := onlyChild(header, nsWSSE, "Security")
	if err != nil {
		return nil, nil, err
	}
	signature, err := onlyChild(securityEl, nsDSig, "Signature")
	if err != nil {
		return nil, nil, err
	}
	if signature == nil {
		return nil, nil, errors.New("message has no signature")
	}
	// Each part of the signature is read from the one element the profile
	// allows, so a duplicate can't hold what another check reads.
	one := func(parent *helium.Element, local string) *helium.Element {
		if err != nil {
			return nil
		}
		var el *helium.Element
		el, err = onlyChild(parent, nsDSig, local)
		return el
	}
	signedInfo := one(signature, "SignedInfo")
	method := one(signedInfo, "CanonicalizationMethod")
	signatureMethod := one(signedInfo, "SignatureMethod")
	reference := one(signedInfo, "Reference")
	transformsEl := one(reference, "Transforms")
	digestMethod := one(reference, "DigestMethod")
	digestValue := one(reference, "DigestValue")
	one(signature, "SignatureValue")
	if err != nil {
		return nil, nil, err
	}
	if alg := algorithm(method); alg != algExcC14N {
		return nil, nil, fmt.Errorf("unsupported canonicalization method %s", alg)
	}
	if alg := algorithm(signatureMethod); alg != algRSASHA256 {
		return nil, nil, fmt.Errorf("unsupported signature method %s", alg)
	}
	bodyID := body.GetAttributeNodeNS("Id", nsWSU)
	if bodyID == nil || bodyID.Value() == "" || attrValue(reference, "URI") != "#"+bodyID.Value() {
		return nil, nil, errors.New("signature must reference the SOAP body")
	}
	transforms := children(transformsEl)
	if len(transforms) != 1 || algorithm(transforms[0]) != algExcC14N {
		return nil, nil, errors.New("body must be transformed with exclusive canonicalization only")
	}
	if _, err := prefixList(method); err != nil {
		return nil, nil, err
	}
	bodyPrefixes, err := prefixList(transforms[0])
	if err != nil {
		return nil, nil, err
	}
	if alg := algorithm(digestMethod); alg != algSHA256 {
		return nil, nil, fmt.Errorf("unsupported digest method %s", alg)
	}
	cert, err := tokenCertificate(securityEl, signature)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := cert.PublicKey.(*rsa.PublicKey); !ok {
		return nil, nil, errors.New("signing certificate must have an RSA key")
	}

	// wsu:Id is an ID by the WS-Security schema, which helium doesn't read.
	bodyID.SetAType(enum.AttrID)
	res, err := xmldsig1.NewVerifier(xmldsig1.StaticKey(cert.PublicKey)).VerifyElement(context.Background(), doc, signature)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid signature: %w", err)
	}
	if len(res.References) != 1 || res.References[0].Element != body {
		return nil, nil, errors.New("signature does not cover the SOAP body")
	}

	// The body is read from its canonical form, checked against the verified
	// digest.
	canonical, err := canonicalBody(body, bodyPrefixes)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(canonical)
	expected, err := decodeBase64(text(digestValue))
	if err != nil || !bytes.Equal(digest[:], expected) {
		return nil, nil, errors.New("canonical body does not match the signed digest")
	}
	return cert, canonical, nil
}

// tokenCertificate reads the certificate from the BinarySecurityToken that
// the signature's KeyInfo points to.
func tokenCertificate(securityEl, signature *helium.Element) (*x509.Certificate, error) {
	keyInfo, err := onlyChild(signature, nsDSig, "KeyInfo")
	if err != nil {
		return nil, err
	}
	tokenRefs, err := onlyChild(keyInfo, nsWSSE, "SecurityTokenReference")
	if err != nil {
		return nil, err
	}
	tokenRef, err := onlyChild(tokenRefs, nsWSSE, "Reference")
	if err != nil {
		return nil, err
	}
	id := strings.TrimPrefix(attrValue(tokenRef, "URI"), "#")
	var token *helium.Element
	for _, el := range children(securityEl) {
		if el.LocalName() != "BinarySecurityToken" || el.URI() != nsWSSE || id == "" || wsuID(el) != id {
			continue
		}
		if token != nil {
			return nil, errors.New("message has more than one BinarySecurityToken with the referenced ID")
		}
		token = el
	}
	if token == nil {
		return nil, errors.New("signing certificate not found")
	}
	if attrValue(token, "ValueType") != tokenValueX509 {
		return nil, errors.New("security token must be an X.509 certificate")
	}
	der, err := decodeBase64(text(token))
	if err != nil {
		return nil, fmt.Errorf("reading security token: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("reading security token: %w", err)
	}
	return cert, nil
}

// canonicalBody returns the exclusive canonical form of the body, the way a
// reference to it with that InclusiveNamespaces prefix list digests it: the
// body is copied into a document of its own, declaring there the inclusive
// prefixes its ancestors put in scope.
func canonicalBody(body *helium.Element, list string) ([]byte, error) {
	prefixes := strings.Fields(list)
	doc := helium.NewDefaultDocument()
	copied, err := helium.CopyNode(body, doc)
	if err != nil {
		return nil, fmt.Errorf("reading SOAP body: %w", err)
	}
	root, ok := copied.(*helium.Element)
	if !ok {
		return nil, errors.New("reading SOAP body")
	}
	if err := doc.SetDocumentElement(root); err != nil {
		return nil, fmt.Errorf("reading SOAP body: %w", err)
	}
	for _, prefix := range prefixes {
		local := prefix
		if local == "#default" {
			local = ""
		}
		if ns := helium.LookupNSByPrefix(body, local); ns != nil {
			if err := root.DeclareNamespace(local, ns.URI()); err != nil {
				return nil, fmt.Errorf("reading SOAP body: %w", err)
			}
		}
	}
	out, err := c14n.NewCanonicalizer(c14n.ExclusiveC14N10).InclusiveNamespaces(prefixes).CanonicalizeTo(doc)
	if err != nil {
		return nil, fmt.Errorf("reading SOAP body: %w", err)
	}
	return out, nil
}
