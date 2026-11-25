package eet

import (
	"context"
	"crypto/x509"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/invopop/gobl"
	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/head"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/xmldsig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No exported entry point panics with a nil or a zero value.
func TestNilAndZeroValues(t *testing.T) {
	var nilClient *Client
	client, err := New()
	require.NoError(t, err)
	signing := &Client{signer: newTestSigner(t), env: EnvironmentPlayground, timeout: MinTimeout, httpClient: &http.Client{}}
	header := &Header{UUID: "03965780-6457-4842-bd80-5f9195c0b8c8"}

	errs := map[string]func() error{
		"New with a nil certificate": func() error { _, err := New(WithCertificate(nil)); return err },
		"New with a new certificate": func() error {
			_, err := New(WithCertificate(new(xmldsig.Certificate)))
			return err
		},
		"New with an empty certificate": func() error {
			_, err := New(WithCertificate(&xmldsig.Certificate{}))
			return err
		},
		"New with a certificate with only a chain": func() error {
			_, err := New(WithCertificate(&xmldsig.Certificate{CaChain: []*x509.Certificate{}}))
			return err
		},
		"New with a zero time": func() error { _, err := New(WithCurrentTime(time.Time{})); return err },
		"NewSale on a nil client": func() error {
			_, err := nilClient.NewSale(loadExampleEnvelope(t, "receipt"))
			return err
		},
		"New with a nil option": func() error { _, err := New(nil); return err },
		"NewSale with a nil option": func() error {
			_, err := client.NewSale(loadExampleEnvelope(t, "receipt"), nil)
			return err
		},
		"Sign on a nil client": func() error { _, err := nilClient.Sign(&Sale{}); return err },
		"Send on a nil client": func() error { _, err := nilClient.Send(context.Background(), &Sale{}); return err },
		"Send on a client not from New": func() error {
			_, err := signing.Send(context.Background(), &Sale{Header: header, Data: &Data{}})
			return err
		},
		"Send with a nil context": func() error {
			_, err := client.Send(nil, &Sale{}) //nolint:staticcheck
			return err
		},
		"NewSale with a nil envelope":    func() error { _, err := client.NewSale(nil); return err },
		"NewSale with an empty envelope": func() error { _, err := client.NewSale(&gobl.Envelope{}); return err },
		"NewSale with a nil stamp": func() error {
			env := loadExampleEnvelope(t, "receipt")
			env.Head.Stamps = append(env.Head.Stamps, nil)
			_, err := client.NewSale(env)
			return err
		},
		"NewSale with no document":          func() error { _, err := client.NewSale(&gobl.Envelope{Head: &head.Header{}}); return err },
		"Sign a nil sale":                   func() error { _, err := signing.Sign(nil); return err },
		"Sign a sale with no header":        func() error { _, err := signing.Sign(&Sale{Data: &Data{}}); return err },
		"Sign a sale with no data":          func() error { _, err := signing.Sign(&Sale{Header: header}); return err },
		"Send a nil sale":                   func() error { _, err := client.Send(context.Background(), nil); return err },
		"Send a sale with no header":        func() error { _, err := client.Send(context.Background(), &Sale{}); return err },
		"ParseResponse with nil data":       func() error { _, err := ParseResponse(nil, sent(errorSchemaUUID), EnvironmentPlayground); return err },
		"ParseResponse with a nil sale":     func() error { _, err := ParseResponse(nil, nil, EnvironmentPlayground); return err },
		"ParseResponse with an empty sale":  func() error { _, err := ParseResponse(nil, &Sale{}, EnvironmentPlayground); return err },
		"ParseResponse with no environment": func() error { _, err := ParseResponse(nil, sent(errorSchemaUUID), ""); return err },
	}
	// These have nothing to refuse, and return a client or a sale.
	succeeds := map[string]bool{
		"New with a nil certificate": true,
		"New with a zero time":       true,
		"New with a nil option":      true,
		"NewSale on a nil client":    true,
		"NewSale with a nil option":  true,
		"NewSale with a nil stamp":   true,
	}
	for name, f := range errs {
		t.Run(name, func(t *testing.T) {
			var err error
			assert.NotPanics(t, func() { err = f() })
			if !succeeds[name] {
				assert.Error(t, err)
			}
		})
	}

	t.Run("signing a sale built by hand with empty data", func(t *testing.T) {
		assert.NotPanics(t, func() { _, _ = signing.Sign(&Sale{Header: header, Data: &Data{}}) })
	})
	t.Run("nil client accessors", func(t *testing.T) {
		assert.NotPanics(t, func() {
			assert.Empty(t, nilClient.Environment())
			assert.WithinDuration(t, time.Now(), nilClient.CurrentTime(), time.Minute)
		})
	})
	t.Run("nil values with methods", func(t *testing.T) {
		var e *Error
		var re *ResponseError
		var d *Data
		assert.NotPanics(t, func() {
			assert.Empty(t, e.Error())
			assert.Empty(t, e.Key())
			assert.Empty(t, e.Code())
			assert.Empty(t, e.Message())
			assert.Nil(t, e.Unwrap())
			assert.False(t, e.Is(ErrServer))
			assert.False(t, ErrServer.Is(e))
			assert.False(t, ErrServer.Is(nil))
			assert.Equal(t, "x", e.WithCode("x").Code())
			assert.Equal(t, "y", e.WithMessage("y").Message())
			assert.Nil(t, re.Err())
			assert.Empty(t, d.SaleKey())
			assert.Empty(t, Environment("").URL())
		})
	})
	t.Run("nil payment", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_, err := new(Client).saleFor(nil)
			assert.ErrorIs(t, err, ErrValidation)
		})
	})
}

// sliceStep is one step from the payment down to a slice of pointers: a
// struct field, an element of a slice, or a nil pointer to fill in.
type sliceStep struct {
	field int
	index int // -1 for a struct field, -2 to fill a nil pointer
}

// pointerSlices finds every slice of pointers in v, at any depth, also under
// nil pointers, walking each struct type once per path.
func pointerSlices(v reflect.Value, path []sliceStep, name string, seen map[reflect.Type]bool, found map[string][]sliceStep) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			pointerSlices(v.Elem(), path, name, seen, found)
			return
		}
		et := v.Type().Elem()
		if et.Kind() != reflect.Struct || seen[et] {
			return
		}
		next := map[reflect.Type]bool{et: true}
		for k := range seen {
			next[k] = true
		}
		pointerSlices(reflect.New(et).Elem(), append(slices.Clone(path), sliceStep{index: -2}), name, next, found)
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Type().Field(i); f.IsExported() {
				pointerSlices(v.Field(i), append(slices.Clone(path), sliceStep{field: i, index: -1}), name+"."+f.Name, seen, found)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Pointer {
			return
		}
		if _, ok := found[name]; !ok {
			found[name] = slices.Clone(path)
		}
		for j := range v.Len() {
			pointerSlices(v.Index(j), append(slices.Clone(path), sliceStep{index: j}), name+"[]", seen, found)
		}
	}
}

// sliceAt follows the path from v, filling in nil pointers on the way.
func sliceAt(v reflect.Value, path []sliceStep) reflect.Value {
	for _, s := range path {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		switch s.index {
		case -2:
		case -1:
			v = v.Field(s.field)
		default:
			v = v.Index(s.index)
		}
	}
	return v
}

// A nil entry in any slice of pointers of a payment, first or last, panics
// nowhere: not in the rules, the helpers or NewSale, in CZK or with an
// exchange rate.
func TestNilEntriesInPaymentSlices(t *testing.T) {
	c, err := New()
	require.NoError(t, err)
	examples := []string{"receipt", "refund", "prepaid-draw", "prepaid-load", "delegated-fuel", "delegated-parking", "delegated-craftsmen"}
	covered := map[string]bool{}
	for _, example := range examples {
		found := map[string][]sliceStep{}
		pointerSlices(reflect.ValueOf(loadExample(t, example)), nil, "payment", map[reflect.Type]bool{}, found)
		for name, path := range found {
			covered[name] = true
			for _, variant := range []string{"first", "last", "first in EUR", "last in EUR"} {
				env := loadExampleEnvelope(t, example)
				pmt := env.Extract().(*bill.Payment)
				if strings.HasSuffix(variant, "EUR") {
					pmt.Currency = currency.EUR
					pmt.ExchangeRates = []*currency.ExchangeRate{{From: currency.EUR, To: currency.CZK, Amount: num.MakeAmount(2500, 2)}}
				}
				list := sliceAt(reflect.ValueOf(pmt), path)
				nilEntry := reflect.Zero(list.Type().Elem())
				if strings.HasPrefix(variant, "first") {
					list.Set(reflect.Append(reflect.MakeSlice(list.Type(), 0, list.Len()+1), append([]reflect.Value{nilEntry}, sliceValues(list)...)...))
				} else {
					list.Set(reflect.Append(list, nilEntry))
				}
				assert.NotPanics(t, func() {
					_ = rules.Validate(pmt)
					addon.SaleTotal(pmt)
					addon.PrepaidDrawn(pmt)
					addon.PrepaidLoaded(pmt)
					addon.SaleNumber(pmt)
					_, _ = c.NewSale(env)
				}, "%s, nil %s in %s", example, variant, name)
			}
		}
	}
	names := slices.Sorted(maps.Keys(covered))
	t.Logf("%d slices of pointers: %s", len(names), strings.Join(names, ", "))
	assert.GreaterOrEqual(t, len(names), 100, "the walk reaches every slice of pointers")
}

func sliceValues(v reflect.Value) []reflect.Value {
	out := make([]reflect.Value, v.Len())
	for i := range out {
		out[i] = v.Index(i)
	}
	return out
}
