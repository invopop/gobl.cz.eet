package eet

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/invopop/gobl"
	"github.com/invopop/gobl/bill"
)

// FuzzNewSale builds payments from bytes, seeded with the examples, and
// takes each through Calculate, the sale builder NewSale uses and Sign: none
// may panic.
func FuzzNewSale(f *testing.F) {
	for _, name := range []string{"receipt", "refund", "prepaid-draw", "prepaid-load", "prepaid-load-with-purchase", "delegated-fuel", "delegated-parking", "delegated-craftsmen"} {
		data, err := os.ReadFile("examples/out/" + name + ".json")
		if err != nil {
			f.Fatal(err)
		}
		env := new(gobl.Envelope)
		if err := json.Unmarshal(data, env); err != nil {
			f.Fatal(err)
		}
		doc, err := json.Marshal(env.Extract())
		if err != nil {
			f.Fatal(err)
		}
		f.Add(doc)
	}
	key, cert := newTestCertificate(f)
	signer, err := newSigner(cert, key)
	if err != nil {
		f.Fatal(err)
	}
	client := &Client{signer: signer}
	f.Fuzz(func(_ *testing.T, data []byte) {
		pmt := new(bill.Payment)
		if err := json.Unmarshal(data, pmt); err != nil {
			return
		}
		_ = pmt.Calculate()
		sale, err := client.saleFor(pmt)
		if err != nil {
			return
		}
		_, _ = client.Sign(sale)
	})
}
