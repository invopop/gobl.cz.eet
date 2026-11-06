package eet

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
)

// envelope is the SOAP request, shaped like the official samples: the
// certificate and the signature in a WS-Security header, and the sale in a
// body that the signature references by its wsu:Id. Digest and signature
// values are filled in once the document is canonicalized.
type envelope struct {
	XMLName xml.Name       `xml:"soapenv:Envelope"`
	SOAPEnv string         `xml:"xmlns:soapenv,attr"`
	V4      string         `xml:"xmlns:v4,attr"`
	Header  envelopeHeader `xml:"soapenv:Header"`
	Body    envelopeBody   `xml:"soapenv:Body"`
}

type envelopeHeader struct {
	Security security `xml:"wsse:Security"`
}

type security struct {
	WSSE      string              `xml:"xmlns:wsse,attr"`
	WSU       string              `xml:"xmlns:wsu,attr"`
	Token     binarySecurityToken `xml:"wsse:BinarySecurityToken"`
	Signature signature           `xml:"ds:Signature"`
}

type binarySecurityToken struct {
	EncodingType string `xml:"EncodingType,attr"`
	ValueType    string `xml:"ValueType,attr"`
	ID           string `xml:"wsu:Id,attr"`
	Value        string `xml:",chardata"`
}

type signature struct {
	ID         string     `xml:"Id,attr"`
	DS         string     `xml:"xmlns:ds,attr"`
	SignedInfo signedInfo `xml:"ds:SignedInfo"`
	Value      string     `xml:"ds:SignatureValue"`
	KeyInfo    keyInfo    `xml:"ds:KeyInfo"`
}

type signedInfo struct {
	CanonicalizationMethod transform `xml:"ds:CanonicalizationMethod"`
	SignatureMethod        method    `xml:"ds:SignatureMethod"`
	Reference              reference `xml:"ds:Reference"`
}

// transform is an exclusive canonicalization with its inclusive namespaces.
type transform struct {
	Algorithm           string              `xml:"Algorithm,attr"`
	InclusiveNamespaces inclusiveNamespaces `xml:"ec:InclusiveNamespaces"`
}

type inclusiveNamespaces struct {
	PrefixList string `xml:"PrefixList,attr"`
	EC         string `xml:"xmlns:ec,attr"`
}

type method struct {
	Algorithm string `xml:"Algorithm,attr"`
}

type reference struct {
	URI          string      `xml:"URI,attr"`
	Transforms   []transform `xml:"ds:Transforms>ds:Transform"`
	DigestMethod method      `xml:"ds:DigestMethod"`
	DigestValue  string      `xml:"ds:DigestValue"`
}

type keyInfo struct {
	ID             string                 `xml:"Id,attr"`
	TokenReference securityTokenReference `xml:"wsse:SecurityTokenReference"`
}

type securityTokenReference struct {
	ID        string         `xml:"wsu:Id,attr"`
	Reference tokenReference `xml:"wsse:Reference"`
}

type tokenReference struct {
	URI       string `xml:"URI,attr"`
	ValueType string `xml:"ValueType,attr"`
}

type envelopeBody struct {
	ID   string `xml:"wsu:Id,attr"`
	WSU  string `xml:"xmlns:wsu,attr"`
	Sale *Sale  `xml:"v4:Trzba"`
}

// newEnvelope prepares the request for the sale, signed by the certificate
// given. Element IDs share the suffix, with the prefixes of the samples.
func newEnvelope(sale *Sale, cert *x509.Certificate, idSuffix string) *envelope {
	tokenID := "X509-" + idSuffix
	bodyID := "id-" + idSuffix
	return &envelope{
		SOAPEnv: nsSOAP,
		V4:      NamespaceEET,
		Header: envelopeHeader{
			Security: security{
				WSSE: nsWSSE,
				WSU:  nsWSU,
				Token: binarySecurityToken{
					EncodingType: tokenEncodingBase64,
					ValueType:    tokenValueX509,
					ID:           tokenID,
					Value:        base64.StdEncoding.EncodeToString(cert.Raw),
				},
				Signature: signature{
					ID: "SIG-" + idSuffix,
					DS: nsDSig,
					SignedInfo: signedInfo{
						CanonicalizationMethod: excC14N(signedInfoPrefixList),
						SignatureMethod:        method{Algorithm: algRSASHA256},
						Reference: reference{
							URI:          "#" + bodyID,
							Transforms:   []transform{excC14N(bodyPrefixList)},
							DigestMethod: method{Algorithm: algSHA256},
						},
					},
					KeyInfo: keyInfo{
						ID: "KI-" + idSuffix,
						TokenReference: securityTokenReference{
							ID: "STR-" + idSuffix,
							Reference: tokenReference{
								URI:       "#" + tokenID,
								ValueType: tokenValueX509,
							},
						},
					},
				},
			},
		},
		Body: envelopeBody{
			ID:   bodyID,
			WSU:  nsWSU,
			Sale: sale,
		},
	}
}

func excC14N(prefixList string) transform {
	return transform{
		Algorithm: algExcC14N,
		InclusiveNamespaces: inclusiveNamespaces{
			PrefixList: prefixList,
			EC:         nsExcC14N,
		},
	}
}
