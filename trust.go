package eet

import (
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed" // certificate authorities
	"encoding/asn1"
	"fmt"
	"slices"
	"strings"
	"time"
)

//go:embed ca/ica-root-ca-rsa-05-2022.pem
var icaRoot []byte

//go:embed ca/ica-public-ca-rsa-06-2022.pem
var icaPublic []byte

// identity is the identity of the certificate that signs the service's
// confirmations in one environment, as the operations documents publish it.
type identity struct {
	commonName     string
	serialNumber   string // not checked when empty
	organizationID string
	organization   string
	country        string
	policy         asn1.ObjectIdentifier
}

// Subject attributes read from the names themselves, as crypto/x509 keeps
// only the last of a repeated one and has no field for the
// organizationIdentifier (2.5.4.97).
var (
	oidCommonName             = asn1.ObjectIdentifier{2, 5, 4, 3}
	oidSerialNumber           = asn1.ObjectIdentifier{2, 5, 4, 5}
	oidOrganizationIdentifier = asn1.ObjectIdentifier{2, 5, 4, 97}
)

// The service's signers, from "Přístupové a provozní informace" v1.2,
// chapter 3.3, for production and for the Playground.
var (
	productionIdentity = identity{
		commonName:     "Spolecne technicke zarizeni spravce dane EET",
		serialNumber:   "ICA - 1275857",
		organizationID: "NTRCZ-72080043",
		organization:   "Generální finanční ředitelství",
		country:        "CZ",
		policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 23624, 10, 1, 71, 1, 1},
	}
	playgroundIdentity = identity{
		commonName:     "NEOSTRE - playground prostredi EET",
		organizationID: "NTRCZ-72080043",
		organization:   "Generální finanční ředitelství",
		country:        "CZ",
		policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 23624, 10, 1, 71, 1, 1},
	}
)

// trust checks a confirmation's signer by the chain, validity, subject and
// policy the operations documents give for the environment (v1.2, chapter
// 3.3); the attributes are checked, not the key, as the certificate may
// change, and revocation is not checked.
type trust struct {
	// playground accepts only the Playground's test confirmations (test
	// true), signed by the Playground's certificate; production ones are
	// refused.
	playground bool

	roots         *x509.CertPool
	intermediates *x509.CertPool

	// identity is the signer the environment's confirmations must have.
	identity identity

	// now is when to check validity, the clock when nil.
	now func() time.Time
}

// defaultTrust accepts the service's confirmations from production, or from
// the Playground. The ca directory has the certificates' sources.
func defaultTrust(playground bool) *trust {
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(icaRoot)
	intermediates := x509.NewCertPool()
	intermediates.AppendCertsFromPEM(icaPublic)
	id := productionIdentity
	if playground {
		id = playgroundIdentity
	}
	return &trust{playground: playground, identity: id, roots: roots, intermediates: intermediates}
}

// check returns an error unless the certificate is the service's signer.
func (tr *trust) check(cert *x509.Certificate) error {
	if tr == nil || tr.roots == nil {
		return ErrUntrusted.WithMessage("no trusted roots to check the response against")
	}
	// The service signs with a general-purpose certificate, with no
	// extended key usage of its own to ask for.
	opts := x509.VerifyOptions{
		Roots:         tr.roots,
		Intermediates: tr.intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	if tr.now != nil {
		opts.CurrentTime = tr.now()
	}
	if _, err := cert.Verify(opts); err != nil {
		return ErrUntrusted.withCause(err)
	}
	return tr.identity.check(cert)
}

// check returns an error naming the first attribute that differs.
func (id *identity) check(cert *x509.Certificate) error {
	subject := cert.Subject
	commonName, oneCommonName := onlyName(subject.Names, oidCommonName)
	serialNumber, oneSerialNumber := onlyName(subject.Names, oidSerialNumber)
	organizationID, oneOrganizationID := onlyName(subject.Names, oidOrganizationIdentifier)
	checks := []struct {
		attribute string
		got       string
		ok        bool
		want      string
	}{
		{"common name", commonName, oneCommonName && commonName == id.commonName, id.commonName},
		{"serial number", serialNumber, id.serialNumber == "" || oneSerialNumber && serialNumber == id.serialNumber, id.serialNumber},
		{"organization identifier", organizationID, oneOrganizationID && organizationID == id.organizationID, id.organizationID},
		{"organization", fmt.Sprint(subject.Organization), slices.Equal(subject.Organization, []string{id.organization}), id.organization},
		{"country", fmt.Sprint(subject.Country), slices.Equal(subject.Country, []string{id.country}), id.country},
		{"policies", fmt.Sprint(cert.Policies), slices.ContainsFunc(cert.Policies, func(oid x509.OID) bool { return oid.EqualASN1OID(id.policy) }), id.policy.String()},
	}
	for _, c := range checks {
		if !c.ok {
			return ErrUntrusted.WithMessage(fmt.Sprintf("response signed by a certificate with %s %q, expected %q", c.attribute, c.got, c.want))
		}
	}
	return nil
}

// onlyName is the value of the subject attribute, and whether there is
// exactly one; with several, the value is all of them.
func onlyName(names []pkix.AttributeTypeAndValue, oid asn1.ObjectIdentifier) (string, bool) {
	var values []string
	for _, name := range names {
		if name.Type.Equal(oid) {
			value, _ := name.Value.(string)
			values = append(values, value)
		}
	}
	return strings.Join(values, ", "), len(values) == 1
}
