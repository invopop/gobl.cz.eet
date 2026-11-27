package eet

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium/xmldsig1"
)

// Namespaces of the SOAP envelope and its WS-Security header.
const (
	nsSOAP = "http://schemas.xmlsoap.org/soap/envelope/"
	nsWSSE = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	nsWSU  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	nsDSig = "http://www.w3.org/2000/09/xmldsig#"
	// nsEET is the namespace of the EET 2.0 messages, schema version 4.
	nsEET = "http://fs.gov.cz/eet/schema/v4"
)

// Algorithms the EET service uses, and the only ones accepted when checking
// a signature (interface description, 5.2). The InclusiveNamespaces element
// of the exclusive canonicalization lives in the algorithm's own namespace.
const (
	algExcC14N   = xmldsig1.ExcC14N10
	algSHA256    = xmldsig1.DigestSHA256
	algRSASHA256 = xmldsig1.AlgRSASHA256
	nsExcC14N    = algExcC14N
)

// Types of the certificate token.
const (
	tokenEncodingBase64 = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"
	tokenValueX509      = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-x509-token-profile-1.0#X509v3"
)

// bodyPrefixList is the InclusiveNamespaces prefix list of the body's
// canonicalization in the official sample requests.
const bodyPrefixList = "v4"

// parseXML reads a message with helium's parser, which refuses external
// entities and network access.
func parseXML(data []byte) (*helium.Document, error) {
	doc, err := helium.NewParser().Parse(context.Background(), data)
	if err != nil {
		return nil, fmt.Errorf("reading message: %w", err)
	}
	if doc.DocumentElement() == nil {
		return nil, errors.New("reading message: no root element")
	}
	return doc, nil
}

// The helpers below take a nil element as an empty one, so reading a message
// that lacks an element gives a clear error further on, not a panic.

// child returns the first child element of el with the namespace and local
// name given, or nil.
func child(el *helium.Element, namespace, local string) *helium.Element {
	for _, c := range children(el) {
		if c.LocalName() == local && c.URI() == namespace {
			return c
		}
	}
	return nil
}

// onlyChild returns the child element of el with the namespace and local name
// given, or nil, and an error when there are several: a second one could
// stand between what is signed and what is read.
func onlyChild(el *helium.Element, namespace, local string) (*helium.Element, error) {
	var found *helium.Element
	for _, c := range children(el) {
		if c.LocalName() != local || c.URI() != namespace {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("message has more than one %s", local)
		}
		found = c
	}
	return found, nil
}

func children(el *helium.Element) []*helium.Element {
	if el == nil {
		return nil
	}
	var elements []*helium.Element
	for n := range helium.Children(el) {
		if c, ok := n.(*helium.Element); ok {
			elements = append(elements, c)
		}
	}
	return elements
}

// attrValue is the value of an attribute without a namespace.
func attrValue(el *helium.Element, name string) string {
	if el == nil {
		return ""
	}
	value, _ := el.GetAttributeNS(name, "")
	return value
}

func text(el *helium.Element) string {
	if el == nil {
		return ""
	}
	return string(el.Content())
}

func algorithm(el *helium.Element) string {
	return attrValue(el, "Algorithm")
}

// prefixList is the InclusiveNamespaces prefix list of a canonicalization
// method or transform, refusing more than one.
func prefixList(method *helium.Element) (string, error) {
	inclusive, err := onlyChild(method, nsExcC14N, "InclusiveNamespaces")
	if err != nil {
		return "", err
	}
	return attrValue(inclusive, "PrefixList"), nil
}

func wsuID(el *helium.Element) string {
	value, _ := el.GetAttributeNS("Id", nsWSU)
	return value
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
}
