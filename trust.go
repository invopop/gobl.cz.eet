package eet

import (
	"crypto/x509"
	_ "embed" // certificate authorities
	"errors"
	"fmt"
	"slices"
	"time"
)

// OrganizationGFR is the organisation in the subject of the certificates
// that sign the service's confirmations: the General Financial Directorate.
const OrganizationGFR = "Generální finanční ředitelství"

//go:embed ca/ica-root-ca-rsa-05-2022.pem
var icaRoot []byte

//go:embed ca/ica-public-ca-rsa-06-2022.pem
var icaPublic []byte

// Trust decides which certificates may sign the service's confirmations:
// those that chain up to one of the Roots, through the Intermediates when
// needed, are valid at the time of the check, and name the Organization in
// their subject. The common name is not checked: it differs between the
// Playground and production and may change on renewal. Nor is revocation:
// an app that needs it fetches and caches I.CA's revocation lists itself.
type Trust struct {
	Roots         *x509.CertPool
	Intermediates *x509.CertPool
	Organization  string

	// Playground accepts only the Playground's test confirmations (test
	// true) instead of production ones. Both environments sign with the
	// same chain and organisation, so this is what keeps a test POK off a
	// real sale.
	Playground bool

	// now is when to check validity, the clock when nil.
	now func() time.Time
}

// DefaultTrust trusts the Financial Administration's certificates issued by
// I.CA Public CA/RSA 06/2022 under I.CA Root CA/RSA 05/2022, the chain that
// signs the Playground's confirmations and that the operations documents
// name for production. It accepts production confirmations only: set
// Playground to work against the Playground. The ca directory has the
// certificates' sources.
func DefaultTrust() *Trust {
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(icaRoot)
	intermediates := x509.NewCertPool()
	intermediates.AppendCertsFromPEM(icaPublic)
	return &Trust{
		Roots:         roots,
		Intermediates: intermediates,
		Organization:  OrganizationGFR,
	}
}

// check returns an error unless the certificate is trusted.
func (tr *Trust) check(cert *x509.Certificate) error {
	if tr == nil || tr.Roots == nil {
		return errors.New("eet: no trusted roots to check the response against")
	}
	opts := x509.VerifyOptions{
		Roots:         tr.Roots,
		Intermediates: tr.Intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	if tr.now != nil {
		opts.CurrentTime = tr.now()
	}
	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("eet: response signed by an untrusted certificate: %w", err)
	}
	if !slices.Contains(cert.Subject.Organization, tr.Organization) {
		return fmt.Errorf("eet: response signed by %q, not %q", cert.Subject.Organization, tr.Organization)
	}
	return nil
}
