package eet

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/uuid"
)

// timeFormat is the schema's dateTime mask: seconds, no fractions, and the
// zone offset.
const timeFormat = "2006-01-02T15:04:05Z07:00"

// prague is the zone of EET times, and of the issue date and time of a
// Czech invoice.
var prague = mustLoadLocation("Europe/Prague")

// Option adjusts the message built for a sale.
type Option func(*options)

type options struct {
	resend       bool
	verification bool
	uuid         string
	now          time.Time
}

// WithResend marks the message as a resend (prvni_zaslani false), for any
// attempt after the first. Resends are due within 48 hours of the sale.
func WithResend() Option {
	return func(o *options) {
		o.resend = true
	}
}

// WithVerification asks the service to check the message without recording
// the sale (overeni): the response carries error code 0 and no POK. Useful
// to test a merchant's setup.
func WithVerification() Option {
	return func(o *options) {
		o.verification = true
	}
}

// NewSale builds the EET data message for a sale recorded as a calculated
// GOBL invoice with the cz-eet-v1 addon. The invoice's issue date and time
// are read as Czech local time. Each call makes a new message, with its own
// UUID and send time, so build one for every attempt, resends included; the
// sale data stays the same across attempts.
func NewSale(inv *bill.Invoice, opts ...Option) (*Sale, error) {
	o := options{
		uuid: uuid.V4().String(),
		now:  time.Now(),
	}
	for _, opt := range opts {
		opt(&o)
	}
	return newSale(inv, o)
}

func newSale(inv *bill.Invoice, o options) (*Sale, error) {
	data, err := newData(inv)
	if err != nil {
		return nil, err
	}
	header := &Header{
		UUID:         o.uuid,
		SentAt:       o.now.In(prague).Format(timeFormat),
		FirstAttempt: !o.resend,
	}
	if o.verification {
		header.Verification = &o.verification
	}
	return &Sale{Header: header, Data: data}, nil
}

func newData(inv *bill.Invoice) (*Data, error) {
	if !slices.Contains(inv.GetAddons(), addon.V1) {
		return nil, errors.New("eet: invoice must use the cz-eet-v1 addon")
	}
	if inv.Totals == nil {
		return nil, errors.New("eet: invoice must be calculated")
	}
	// The addon rules catch these too; checked again here because the
	// message would still build, misstating the sale.
	if !inv.Type.In(bill.InvoiceTypeStandard, bill.InvoiceTypeCreditNote) {
		return nil, errors.New("eet: invoice type must be standard or credit-note")
	}
	if inv.Supplier == nil || inv.Supplier.TaxID == nil || inv.Supplier.TaxID.Code == "" {
		return nil, errors.New("eet: invoice needs the supplier's tax ID")
	}
	if inv.Supplier.TaxID.Country != l10n.TaxCountryCode(l10n.CZ) {
		return nil, errors.New("eet: supplier tax ID must be Czech")
	}
	if inv.IssueTime == nil {
		return nil, errors.New("eet: invoice needs an issue time")
	}
	unit := inv.Tax.GetExt(addon.ExtKeyUnit)
	device := inv.Tax.GetExt(addon.ExtKeyDevice)
	if unit == "" || device == "" {
		return nil, errors.New("eet: invoice needs the cz-eet-unit and cz-eet-device extensions")
	}
	total, err := saleTotal(inv)
	if err != nil {
		return nil, err
	}
	return &Data{
		TaxID:    "CZ" + inv.Supplier.TaxID.Code.String(),
		UnitID:   unit.String(),
		DeviceID: device.String(),
		Number:   addon.SaleNumber(inv),
		SoldAt:   soldAt(inv).Format(timeFormat),
		Total:    total,
	}, nil
}

// soldAt reads the issue date and time as Czech local time.
func soldAt(inv *bill.Invoice) time.Time {
	d, t := inv.IssueDate, inv.IssueTime
	return cal.MakeDateTime(d.Year, d.Month, d.Day, t.Hour, t.Minute, t.Second).In(prague)
}

// saleTotal is the amount payable in CZK with two decimals, negative for a
// refund. A zero refund stays "0.00": the schema has no "-0.00".
func saleTotal(inv *bill.Invoice) (string, error) {
	total := currency.Convert(inv.ExchangeRates, inv.Currency, currency.CZK, inv.Totals.Payable)
	if total == nil {
		return "", errors.New("eet: invoice needs an exchange rate to CZK")
	}
	amount := total.Rescale(2)
	if inv.Type == bill.InvoiceTypeCreditNote && !amount.IsZero() {
		amount = amount.Negate()
	}
	return amount.String(), nil
}

// SaleKey identifies a sale: its six values joined with '|'. Messages with
// the same key are attempts to record the same sale, so the key tells a
// duplicate confirmation apart from a new sale.
func (d *Data) SaleKey() string {
	return strings.Join([]string{d.TaxID, d.UnitID, d.DeviceID, d.Number, d.SoldAt, d.Total}, "|")
}

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
