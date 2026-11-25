package eet

import (
	"flag"
	"time"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/uuid"
)

// update rewrites the golden output of the tests that have one.
var update = flag.Bool("update", false, "update golden test output")

// Update gives the black-box tests the same flag.
var Update = update

// saleFor builds a sale from a payment with a new UUID and the current time.
func saleFor(pmt *bill.Payment, opts ...GenerateOption) (*Sale, error) {
	o := generateOptions{uuid: uuid.V4().String()}
	for _, opt := range opts {
		opt(&o)
	}
	sale, err := newSale(pmt, o)
	if err != nil {
		return nil, err
	}
	sale.Header.SentAt = sentAt(time.Now())
	return sale, nil
}
