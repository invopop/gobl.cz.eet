# GOBL ➡️ Czech EET 2.0

Go library to convert [GOBL](https://github.com/invopop/gobl) payments into
Czech EET 2.0 sale messages and send them to the Financial Administration:
the `cz-eet-v1` addon, a converter from payments to the EET data message, a
WS-Security signer, and a reader for its responses.

Released under the Apache 2.0 [LICENSE](https://github.com/invopop/gobl.cz.eet/blob/main/LICENSE), Copyright 2026 [Invopop S.L.](https://invopop.com).

[![Lint](https://github.com/invopop/gobl.cz.eet/actions/workflows/lint.yaml/badge.svg)](https://github.com/invopop/gobl.cz.eet/actions/workflows/lint.yaml)
[![Test Go](https://github.com/invopop/gobl.cz.eet/actions/workflows/test.yaml/badge.svg)](https://github.com/invopop/gobl.cz.eet/actions/workflows/test.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/invopop/gobl.cz.eet)](https://goreportcard.com/report/github.com/invopop/gobl.cz.eet)
[![codecov](https://codecov.io/gh/invopop/gobl.cz.eet/graph/badge.svg)](https://codecov.io/gh/invopop/gobl.cz.eet)
[![GoDoc](https://godoc.org/github.com/invopop/gobl.cz.eet?status.svg)](https://godoc.org/github.com/invopop/gobl.cz.eet)
![Latest Tag](https://img.shields.io/github/v/tag/invopop/gobl.cz.eet)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/invopop/gobl.cz.eet)

## EET 2.0 in short

From 1 January 2027, payments taken in person in the Czech Republic (cash,
card at the counter, vouchers and similar) are sent to the Financial
Administration as signed data messages, `Trzba`, and it answers each with a
confirmation code, the POK. The law sets which payments and taxpayers this
covers, and exempts some. EET calls each of these payments a sale (*tržba*),
so here "sale" is the message and "payment" the GOBL document. The message has
no lines, taxes or customer: only the merchant's tax ID, the recording unit
and device the payment was taken on, its sequence number and time, and the
amount received in CZK. Each merchant signs with their own certificate from
the CA EET. The interface description, schema and samples are
on [eet.gov.cz](https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0).

## The addon

Import the addon to register it, then declare `cz-eet-v1` on a payment
receipt, one for every payment taken in person:

```go
import _ "github.com/invopop/gobl.cz.eet/addon"
```

```yaml
$schema: "https://gobl.org/draft-0/bill/payment"
$addons:
  - "cz-eet-v1"
series: "PRG"
code: "000135"
issue_date: "2027-01-09"
issue_time: "16:45:36"
ext:
  cz-eet-unit: "303"
  cz-eet-device: "POS-1"

supplier:
  name: "Kavárna u Mostu s.r.o."
  tax_id:
    country: "CZ"
    code: "25596641"

lines:
  - amount: "205.00"

methods:
  - key: "cash"
```

That is all a payment needs. GOBL fills in the rest when it calculates the
payment: the `receipt` type, the regime and the currency from the supplier's
tax ID, the amount of a single method, and the total.

| Key | Kind | EET item |
| --- | --- | --- |
| `cz-eet-unit` | Extension in the payment's `ext` | `id_jednotky`, the recording unit number from DIS+ |
| `cz-eet-device` | Extension in the payment's `ext` | `id_pokl`, the end device within the unit |
| `cz-eet-prepaid` | Extension in a method's `ext`, value `draw` | `cerp_zuct`, the part drawn from prepaid credit |
| `cz-eet-prepaid` | Extension in the payment's `ext`, value `load` | `urceno_cerp_zuct`, the total, loaded onto prepaid credit |
| `cz-eet-delegation` | Extension in the payment's `ext`, value `one` or `many` | `eic_poverujiciho` and `povereni_vice_popl`, for a sale recorded under a delegation |
| `eet-pok` | Stamp | `pok`, the confirmation code |

The sale total is the payment's total, which GOBL makes negative for lines
marked `refund`: a refund is a receipt with refund lines, and an exchange is
one receipt with a refund line for the item taken back and another line for
the new one, so the sale is the difference paid. The Financial Administration
also accepts an exchange sent as two messages, the refund and the new sale,
which suits a till that can't net them. An exchange at the same price nets
to 0.00, which EET accepts; for a sale that nets to zero, the Financial
Administration recommends two messages instead (item description,
returnable packaging, example 3).

The rules ask for what the message needs and a payment may lack: the
`receipt` type, a Czech supplier tax ID, an issue time, both extensions, CZK
or an exchange rate to CZK, a series and code that fit `porad_cis`, and a
total, and a part drawn from prepaid credit, within ±99,999,999.99 CZK. A
method dated on another day than the payment is refused, as each payment is
recorded on its own, and so is a payment with an ordering issuer but without
the delegation extension. A total that is not the sum
of the lines is refused too, as on a payment that was never calculated or was
changed afterwards. Full examples are in [`examples`](./examples).

The issue date and time are the moment the money was received, in **Czech
local time** (Europe/Prague), which EET records with seconds. An issue time
taken from a server clock in another zone records the sale at the wrong hour
without any error.

The addon has no rules for invoices, and `NewSale` only takes a payment: EET
records each payment, not each invoice. One payment document is one payment
taken in person. An advance payment and the final payment of an invoice are
two payments, each with its own receipt, and money received another way, such as
a bank transfer, gets no receipt with the addon.

## Development

```sh
go test ./...
go test . -update      # rewrite the golden files in examples/out
```

The signer is tested against the three official signed sample requests in
[`test/samples`](./test/samples), whose bodies it digests identically, and
the response reader against Playground replies in
[`test/responses`](./test/responses). In the tests, signatures are also
checked by a second, independent judge built on goxmldsig.
