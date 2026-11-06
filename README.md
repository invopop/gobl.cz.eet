# GOBL ⬅️➡️ Czech EET 2.0

Czech EET 2.0 sales recording for [GOBL](https://github.com/invopop/gobl): the
`cz-eet-v1` addon, a converter from invoices to the EET data message, a
WS-Security signer, and a reader for the service's responses.

Released under the Apache 2.0 [LICENSE](https://github.com/invopop/gobl.cz.eet/blob/main/LICENSE), Copyright 2026 [Invopop S.L.](https://invopop.com).

[![Lint](https://github.com/invopop/gobl.cz.eet/actions/workflows/lint.yaml/badge.svg)](https://github.com/invopop/gobl.cz.eet/actions/workflows/lint.yaml)
[![Test Go](https://github.com/invopop/gobl.cz.eet/actions/workflows/test.yaml/badge.svg)](https://github.com/invopop/gobl.cz.eet/actions/workflows/test.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/invopop/gobl.cz.eet)](https://goreportcard.com/report/github.com/invopop/gobl.cz.eet)
[![codecov](https://codecov.io/gh/invopop/gobl.cz.eet/graph/badge.svg)](https://codecov.io/gh/invopop/gobl.cz.eet)
[![GoDoc](https://godoc.org/github.com/invopop/gobl.cz.eet?status.svg)](https://godoc.org/github.com/invopop/gobl.cz.eet)
![Latest Tag](https://img.shields.io/github/v/tag/invopop/gobl.cz.eet)

## EET 2.0 in short

From 1 January 2027, every sale paid in person in the Czech Republic (cash,
card at the counter, vouchers and similar) is sent to the Financial
Administration as a signed data message, `Trzba`, which answers with a
confirmation code, the POK. The message has no lines, taxes or customer: only
the merchant's tax ID, the recording unit and device of the sale, its sequence
number and time, and the total in CZK. Each merchant signs with their own
certificate from the CA EET. The interface description, schema and samples are
on [eet.gov.cz](https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0).

## The addon

Import the addon to register it, then declare `cz-eet-v1` on a standard or
simplified invoice, or a credit note for a refund:

```go
import _ "github.com/invopop/gobl.cz.eet/addon"
```

```yaml
$schema: "https://gobl.org/draft-0/bill/invoice"
$regime: "CZ"
$addons:
  - "cz-eet-v1"
series: "PRG"
code: "000135"
issue_date: "2027-01-09"
issue_time: "16:45:36"
tax:
  ext:
    cz-eet-unit: "303"
    cz-eet-device: "POS-1"
supplier:
  name: "Kavárna u Mostu s.r.o."
  tax_id:
    country: "CZ"
    code: "12345679"
# ...
```

| Key | Kind | EET item |
| --- | --- | --- |
| `cz-eet-unit` | Extension in `tax.ext` | `id_jednotky`, the recording unit number from DIS+ |
| `cz-eet-device` | Extension in `tax.ext` | `id_pokl`, the end device within the unit |
| `cz-eet-pok` | Stamp | `pok`, the confirmation code |

The rules ask for what the message needs and an invoice may lack: a Czech
supplier tax ID, an issue time, both extensions, CZK or an exchange rate to
CZK, a series and code that fit `porad_cis`, and a total within
±99,999,999.99 CZK. Full examples are in [`examples`](./examples).

The issue date and time are the moment of the sale in **Czech local time**
(Europe/Prague), which EET records with seconds. An issue time taken from a
server clock in another zone records the sale at the wrong hour without any
error.

## Sending a sale

```go
import (
	eet "github.com/invopop/gobl.cz.eet"
	_ "github.com/invopop/gobl.cz.eet/addon"
)

signer, err := eet.NewSignerFromPKCS12(p12, password) // the merchant's CA EET certificate

sale, err := eet.NewSale(inv)  // a calculated invoice with the cz-eet-v1 addon
request, err := signer.Sign(sale)

// POST the request over HTTP/1.1 with these headers, eet.SOAPAction quoted:
//   Content-Type: text/xml; charset=utf-8
//   SOAPAction: "http://fs.gov.cz/eet/OdeslaniTrzby"
// then read the answer:
res, err := eet.ParseResponse(body, eet.DefaultTrust())
switch {
case err != nil:
	// a SOAP fault, an unreadable answer, or a confirmation that isn't
	// genuine: nothing to keep, send again later
case res.Header == nil || res.Header.UUID != sale.Header.UUID:
	// not the answer to this message: don't keep anything from it
case res.Confirmation != nil:
	// keep res.Confirmation.POK as the cz-eet-pok stamp
case res.Error.Code.Retry():
	// send again later, within 48 hours: eet.NewSale(inv, eet.WithResend())
default:
	// fix the data or the setup: res.Error says what failed
}
```

`NewSale` makes a new message, with its own UUID and send time, on every call,
so build one per attempt. `Data.SaleKey()` identifies the sale across attempts.
`WithVerification()` asks the service to check a message without recording it.

`ParseResponse` only accepts a confirmation signed by a certificate the trust
accepts. `DefaultTrust` is the I.CA chain described in [`ca`](./ca) and accepts
production confirmations only; set its `Playground` field when sending to the
Playground, whose replies are signed under the same chain. Build your own
`Trust` to change the roots or the expected organisation. Revocation is not
checked: an app that needs it fetches I.CA's revocation lists itself.

## Development

```sh
go test ./...          # the XSD checks need libxml2 (apt install libxml2-dev)
go test . -update      # rewrite the golden files in examples/out
```

The signer is tested against the three official signed sample requests in
[`test/samples`](./test/samples), and the response reader against Playground
replies in [`test/responses`](./test/responses).
