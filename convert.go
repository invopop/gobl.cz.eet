package eet

import (
	"time"
	_ "time/tzdata" // Prague's zone, also where the system has none

	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/rules"
)

// timeFormat is the schema's dateTime mask: seconds, no fractions, and the
// zone offset. wallClockFormat is the same without the offset.
const (
	timeFormat      = "2006-01-02T15:04:05Z07:00"
	wallClockFormat = "2006-01-02T15:04:05"
)

// prague is the zone of EET times and of a Czech payment's issue date and
// time.
var prague = mustLoadLocation("Europe/Prague")

// GenerateOption adjusts the message built for a sale.
type GenerateOption func(*generateOptions)

type generateOptions struct {
	resend       bool
	verification bool
	uuid         string
}

// WithResend marks the message as a resend (prvni_zaslani false), for every
// attempt after the first.
func WithResend() GenerateOption {
	return func(o *generateOptions) {
		o.resend = true
	}
}

// WithVerification asks the service to check the message without recording
// the sale (overeni): the response carries error code 0 and no POK.
func WithVerification() GenerateOption {
	return func(o *generateOptions) {
		o.verification = true
	}
}

func newSale(pmt *bill.Payment, o generateOptions) (*Sale, error) {
	data, err := newData(pmt)
	if err != nil {
		return nil, err
	}
	header := &Header{UUID: o.uuid, FirstAttempt: !o.resend}
	if o.verification {
		header.Verification = &o.verification
	}
	return &Sale{Header: header, Data: data}, nil
}

// sentAt is the send time (dat_odesl) as the schema takes it, in Czech
// local time.
func sentAt(t time.Time) string {
	return t.In(prague).Format(timeFormat)
}

func newData(pmt *bill.Payment) (*Data, error) {
	if pmt == nil {
		return nil, ErrValidation.WithMessage("no payment")
	}
	// The addon's rules only apply to a payment that declares it.
	if !addon.V1.In(pmt.GetAddons()...) {
		return nil, ErrValidation.WithMessage("missing '" + addon.V1.String() + "' addon")
	}
	if err := rules.Validate(pmt); err != nil {
		return nil, ErrValidation.WithMessage("payment is not valid: " + err.Error()).withCause(err)
	}
	// Guards against a rule change: validation leaves none of these empty.
	total := addon.SaleTotal(pmt)
	if pmt.Supplier == nil || pmt.Supplier.TaxID == nil || pmt.IssueTime == nil || total == nil {
		return nil, ErrValidation.WithMessage("payment is missing what validation should have required")
	}
	data := &Data{
		TaxID:    pmt.Supplier.TaxID.String(),
		UnitID:   pmt.Ext.Get(addon.ExtKeyUnit).String(),
		DeviceID: pmt.Ext.Get(addon.ExtKeyDevice).String(),
		Number:   addon.SaleNumber(pmt),
		SoldAt:   soldAt(pmt).Format(timeFormat),
		Total:    total.String(),
	}
	// Under a delegation the ordering issuer records the sale, which is the
	// supplier's income (item description, chapter 3, situations 4 and 5).
	if pmt.Ordering != nil && pmt.Ordering.Issuer != nil {
		issuer := pmt.Ordering.Issuer
		if issuer.TaxID == nil {
			return nil, ErrValidation.WithMessage("payment is missing what validation should have required")
		}
		data.TaxID = issuer.TaxID.String()
		data.DelegatingTaxID = pmt.Supplier.TaxID.String()
	}
	data.DelegatedByMany = pmt.Ext.Get(addon.ExtKeyDelegation) == addon.DelegationMany
	// Items that don't apply are left out, not sent as zero (item
	// description, common information).
	if loaded := addon.PrepaidLoaded(pmt); loaded != nil && !loaded.IsZero() {
		data.PrepaidLoaded = loaded.String()
	}
	if drawn := addon.PrepaidDrawn(pmt); drawn != nil && !drawn.IsZero() {
		data.PrepaidDrawn = drawn.String()
	}
	return data, nil
}

// soldAt reads the issue date and time as Czech local time: like RFC 5545
// (3.3.5), the hour that repeats when summer time ends is read as its first
// occurrence, and a time skipped when it starts with the offset before the
// gap, an hour later on the clock.
func soldAt(pmt *bill.Payment) time.Time {
	d, t := pmt.IssueDate, pmt.IssueTime
	at := cal.MakeDateTime(d.Year, d.Month, d.Day, t.Hour, t.Minute, t.Second).In(prague)
	if earlier := at.Add(-time.Hour); earlier.Format(wallClockFormat) == at.Format(wallClockFormat) {
		return earlier
	}
	return at
}

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
