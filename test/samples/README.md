# Official sample requests

The three signed sample requests of EET 2.0, from the technical documentation
the Financial Administration publishes on
[Dokumenty k EET 2.0](https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0). They
are unedited copies, last checked against that page on 2026-10-15.

| File | SHA-256 |
| --- | --- |
| `CZ00000019.eet.v4.req.xml` | `4392b4ca0e6eb586c10b6f80d8946f5269f500a5b2b0d94fa7a092ace867ade5` |
| `CZ683555118.eet.v4.req.xml` | `a77d28641603ffd85a3bd9eff580f554e5e822741cd0594d84b48b52e7874b75` |
| `CZ8551015704.eet.v4.req.xml` | `a61b6ac6cc50b908d68550aa4981b91df0f5d3cbfe564b5d60b545ef70a06bd6` |

The signer tests verify the official signatures, and build each sample's sale
again to check that its body gives the same digest.

To check the files again:

```sh
sha256sum test/samples/*.xml
```
