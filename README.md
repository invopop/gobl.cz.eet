# GOBL ➡️ Czech EET 2.0

Go library to convert [GOBL](https://github.com/invopop/gobl) payments into
Czech EET 2.0 sale messages and send them to the Financial Administration:
the `cz-eet-v1` addon, a converter from payments to the EET data message, a
WS-Security signer, a client for the service, and a reader for its
responses.

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
| `prepaid` | Payment method key qualifier, as in `other+prepaid` | `cerp_zuct`, the part drawn from prepaid credit |
| `cz-eet-prepaid-load` | Extension in a line's `ext`, value `true` or `false` | `urceno_cerp_zuct`, the part loaded onto prepaid credit |
| `ordering.issuer` | Party in the payment | `eic_popl` and `eic_poverujiciho`, for a sale recorded under a delegation |
| `cz-eet-multiple-taxpayers` | Extension in the payment's `ext`, value `true` or `false` | `povereni_vice_popl`, for a sale that is income of several taxpayers |
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
`receipt` type, a Czech supplier tax ID, an issue time, the unit and device
extensions, CZK or an exchange rate to CZK, a series and code that fit
`porad_cis`, and a total, and parts drawn from or loaded onto prepaid credit,
within ±99,999,999.99 CZK. The two yes-or-no extensions take `true` or
`false`, each in its own place: `cz-eet-prepaid-load` on a line and
`cz-eet-multiple-taxpayers` on the payment. A
method dated on another day than the payment is refused, as each payment is
recorded on its own. A total that is not the sum
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

## Usage

### Go Package

You must have first created a GOBL envelope containing a payment receipt
with the `cz-eet-v1` addon, calculated, for each payment taken in person. The
module doesn't write to the envelope: keep the POK it returns as the
envelope's `eet-pok` stamp, which the addon defines, on a signed envelope, as
GOBL only accepts stamps on one.

```go
import (
	"context"
	"errors"

	eet "github.com/invopop/gobl.cz.eet"
	_ "github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/xmldsig"
)

// The merchant's certificate, the .p12 file the CA EET issues.
cert, err := xmldsig.LoadCertificate("./CA_EET.p12", password)
if err != nil {
	return err
}

client, err := eet.New(
	eet.WithCertificate(cert),
	eet.InProduction(), // the Playground by default
)
if err != nil {
	return err
}

var res *eet.Response
sale, err := client.NewSale(env) // validates the payment in the envelope
if err == nil {
	res, err = client.Send(ctx, sale)
}
switch {
case err == nil && res.Confirmation != nil:
	// recorded: keep res.Confirmation.POK as the envelope's eet-pok stamp
case err == nil:
	// verification mode: the message passed the checks, nothing recorded
case errors.Is(err, eet.ErrUntrusted), errors.Is(err, eet.ErrEnvironment):
	// a validly signed confirmation whose certificate isn't the environment's
	// signer or couldn't be checked (a clock set wrong, say), or one from the
	// other environment: no valid POK, so send again with eet.WithResend(),
	// and have someone check the certificate, the clock or where it was sent
case errors.Is(err, eet.ErrConnection), errors.Is(err, eet.ErrServer):
	// no answer, one we can't read, or one that says to try later: send
	// again later with client.NewSale(env, eet.WithResend())
case errors.Is(err, eet.ErrDuplicate):
	// the envelope has a POK for this environment: nothing to send
case errors.Is(err, eet.ErrValidation):
	// the payment, the certificate or the request needs fixing before it
	// can go: the error's code and message say what failed
}
```

`Send` signs the sale, posts it over HTTP/1.1 and TLS 1.2 or higher,
verifying the server's certificate, and reads the reply with `ParseResponse`.
An error code in the reply (Chyba) comes back as an error with the code:
`ErrServer` for the codes to send again later (8, and every negative code:
-1 is a temporary error and -999 to -2 are reserved next to it, interface
description, 3.5.4), `ErrValidation` for the rest. A SOAP fault is
`ErrServer` for a `Server` fault code, or a more specific one such as
`Server.Busy`, and `ErrValidation` for any other. A SOAP envelope is read as the reply whatever
the HTTP status; any other body with a status other than 2xx, such as an
error page, is `ErrServer`, 4xx included: the service reports what is wrong
with a message with an error code or a fault, so such a page comes from the
infrastructure in front of it, and the sale is sent again. A
network error, the time limit passing, or a reply that can't be read or
answers another message is `ErrConnection`.

`Response.TransactionID` is the `X-Global-Transaction-Id` the service tags
the exchange with, for tracing it with the Financial Administration
(interface description, 5.4); errors about a reply carry it too, when the
service sent one. The client waits 10 seconds for a reply by default; `WithTimeout`
changes that, but not below the 2 seconds the EET Act sets (`MinTimeout`).
The client speaks HTTP/1.1 over TLS 1.2 or higher, follows no redirects, and
takes a proxy from the environment (`HTTPS_PROXY`). With `DEBUG=true` in the environment, the client
logs each request with its body, and the status and headers of each reply;
the reply's body isn't logged, as the client reads it itself. An app that
posts the request itself gets it from `client.Sign(sale)` and reads the reply
with `eet.ParseResponse(data, sale, client.Environment())`.

`NewSale` expects a payment that has been calculated. It validates the
payment itself and refuses it with the faults when a rule doesn't pass, so a
payment whose total isn't the sum of its lines, or that EET would take with
the wrong number, never builds a message.

`NewSale` fills `cerp_zuct` from the methods whose key has `prepaid`, such as
`other+prepaid`, leaving it out when they add up to zero, and
`urceno_cerp_zuct` from the lines marked `cz-eet-prepaid-load: "true"`, refund
lines subtracted, left out when they add up to zero. A load and a purchase
paid together (item 14, example 1) are one payment with a line for each: a
wallet topped up with 1,000 CZK and a drink for 50 CZK are sent as
`celk_trzba="1050.00"` and `urceno_cerp_zuct="1000.00"`. A line or payment
without the extension counts as `"false"`.

A sale recorded under a delegation to record sales (pověření, § 10 of the EET
Act; item description, chapter 3, situations 4 and 5) has the taxpayer whose
income it is as the supplier and whoever records it as the ordering issuer
(`ordering.issuer`, the third party who issues the document):

- With an issuer, the issuer records it for the supplier. `eic_popl` is the
  issuer's tax ID and `eic_poverujiciho` the supplier's.
- Marked `cz-eet-multiple-taxpayers: "true"`, the sale is income of several
  taxpayers, and `povereni_vice_popl` is sent. With an issuer, `eic_popl` is
  the issuer's tax ID and `eic_poverujiciho` the supplier's, as the Financial
  Administration recommends; without one, the supplier records it and
  `eic_popl` is theirs.

Whoever records the sale sends it from their own recording unit
(`cz-eet-unit`) and signs it with their own certificate, whose tax ID should
be the one in `eic_popl`, or the service adds warning 1. The payee, who receives the money, isn't part of the
message.

`NewSale` makes a new message, with its own UUID, on every call, and `Sign`
sets its send time and signs it once, so build one per attempt;
`WithCurrentTime` fixes the send time in tests. A UUID is never used twice: it
is the only thing that ties a reply to its message. `Data.SaleKey()` identifies the sale across attempts.
`WithVerification()` asks the service to check a message without recording it.

A payment is only sent again when no POK came back for it (item description,
chapter 3), so `NewSale` refuses an envelope that already has the `eet-pok`
stamp with `ErrDuplicate`, except in verification mode, which records
nothing. A Playground POK, ending in `-ff`, isn't valid under the EET Act
(interface description, 3.4.2.3), so it only blocks in the Playground. An
empty `eet-pok` stamp, or more than one, is refused with `ErrValidation`.

Every attempt after the first is built with `WithResend()`, also when the
first got no answer or an answer that `ParseResponse` refused. A sale that
could not be recorded is sent again without delay once the cause has passed,
at the latest 48 hours after the sale, and it still has to be recorded if that
limit is missed (§ 19 of the EET Act; item description, chapter 6).

Sending again is always safe: a message with the same sale data as one
already received counts as the same sale (interface description, chapter 4,
on `eic_popl`, `id_jednotky`, `id_pokl`, `porad_cis`, `dat_trzby` and
`celk_trzba`), and a sale is sent again whenever no POK came back (item
description, chapter 3). `ErrUntrusted` and `ErrEnvironment` say the reply
came back with a valid signature but can't be trusted: its certificate isn't
the environment's signer, the client couldn't check it (a clock set wrong
makes the certificate look expired or not yet valid), or it comes from the
other environment. A missing or broken signature is `ErrConnection`. No valid POK came back, so the sale is sent again with
`WithResend()`; these errors stay apart from `ErrConnection` so that the app
can also have someone look at the certificate, the clock or the
environment, as they repeat until the cause is fixed.

The client's environment, production or the Playground, gives both the URL
to send to and the checks on the reply, so the two can't be mixed;
`ParseResponse` takes it as an `Environment`. Both come from the operations
documents ("Přístupové a provozní informace" v1.2, for [production](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_produk-5.pdf#page=7)
and the [Playground](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_playgr-5.pdf#page=7)): the URL from chapter 2.1, the signer from
chapter 3.3.

`ParseResponse` only accepts a confirmation signed by the environment's
certificate, as the [interface description](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_popis_rozhrani_v1.2.pdf#page=37) sets out (section 5.3),
for the very message it is given, checked by chain, validity, subject and
policy as [`ca`](./ca) sets out from chapter 3.3 of the operations documents
([production](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_produk-5.pdf#page=7), [Playground](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_playgr-5.pdf#page=7)). The service may change the
certificate at any time while keeping those attributes
([developer seminar slides](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET2_Prezentace_Seminar_pro_vyvojare_23-.pdf#page=44), page 44), so they are what is checked,
not its key.

The certificate is loaded with [xmldsig](https://github.com/invopop/xmldsig),
as in the other GOBL modules, but signing and verification use
[helium](https://github.com/lestrrat-go/helium), as xmldsig only has the XAdES
profiles and EET asks for WS-Security with a BinarySecurityToken. The module
adds the WS-Security envelope and the interface description's checks on the
signature's shape (5.2).

The client and the response checks rely on Go's HTTP, certificate and XML
code, so the module asks for Go 1.26.11, the first release that fixes what
govulncheck finds reachable from them. Build the app with the latest Go 1.26
patch release.

## Limitations

- The response signer's certificate is not checked against I.CA's revocation
  lists.
- The recorded replies the tests read come from the Playground only: the
  production signer's identity comes from the [operations document](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_produk-5.pdf#page=7)
  (chapter 3.3), and no production reply has been read yet.
- One sale per message, as EET takes them; retries, the 48-hour limit and
  keeping track of which sales are confirmed are left to the app.

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
