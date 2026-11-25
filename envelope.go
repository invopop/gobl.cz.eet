package eet

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
)

// soapEnvelope is the SOAP request before it is signed.
type soapEnvelope struct {
	XMLName xml.Name   `xml:"soapenv:Envelope"`
	SOAPEnv string     `xml:"xmlns:soapenv,attr"`
	V4      string     `xml:"xmlns:v4,attr"`
	Header  soapHeader `xml:"soapenv:Header"`
	Body    soapBody   `xml:"soapenv:Body"`
}

type soapHeader struct {
	Security security `xml:"wsse:Security"`
}

type security struct {
	WSSE  string              `xml:"xmlns:wsse,attr"`
	WSU   string              `xml:"xmlns:wsu,attr"`
	Token binarySecurityToken `xml:"wsse:BinarySecurityToken"`
}

type binarySecurityToken struct {
	EncodingType string `xml:"EncodingType,attr"`
	ValueType    string `xml:"ValueType,attr"`
	ID           string `xml:"wsu:Id,attr"`
	Value        string `xml:",chardata"`
}

type soapBody struct {
	ID   string `xml:"wsu:Id,attr"`
	WSU  string `xml:"xmlns:wsu,attr"`
	Sale *Sale  `xml:"v4:Trzba"`
}

// IDs of the elements the signature and its KeyInfo refer to, sharing a
// suffix with the prefixes of the official samples.
type soapEnvelopeIDs struct {
	body, token, signature, keyInfo, tokenReference string
}

func newSOAPEnvelopeIDs(suffix string) soapEnvelopeIDs {
	return soapEnvelopeIDs{
		body:           "id-" + suffix,
		token:          "X509-" + suffix,
		signature:      "SIG-" + suffix,
		keyInfo:        "KI-" + suffix,
		tokenReference: "STR-" + suffix,
	}
}

func newSOAPEnvelope(sale *Sale, cert *x509.Certificate, ids soapEnvelopeIDs) *soapEnvelope {
	return &soapEnvelope{
		SOAPEnv: nsSOAP,
		V4:      nsEET,
		Header: soapHeader{
			Security: security{
				WSSE: nsWSSE,
				WSU:  nsWSU,
				Token: binarySecurityToken{
					EncodingType: tokenEncodingBase64,
					ValueType:    tokenValueX509,
					ID:           ids.token,
					Value:        base64.StdEncoding.EncodeToString(cert.Raw),
				},
			},
		},
		Body: soapBody{
			ID:   ids.body,
			WSU:  nsWSU,
			Sale: sale,
		},
	}
}
