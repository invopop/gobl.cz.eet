package eet

import (
	"flag"

	"github.com/invopop/gobl/bill"
)

// update rewrites the golden output of the tests that have one.
var update = flag.Bool("update", false, "update golden test output")

// Update gives the black-box tests the same flag.
var Update = update

// saleFor builds a sale from a payment as NewSale does from its envelope,
// with the send time set as Sign would.
func (c *Client) saleFor(pmt *bill.Payment, opts ...GenerateOption) (*Sale, error) {
	sale, err := newSale(pmt, c.generateOptions(opts))
	if err != nil {
		return nil, err
	}
	sale.Header.SentAt = sentAt(c.CurrentTime())
	return sale, nil
}
