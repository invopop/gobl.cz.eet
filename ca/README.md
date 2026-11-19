# Certificate authorities

The EET service signs its confirmations with a certificate of the Financial
Administration (O = Generální finanční ředitelství) issued by I.CA (První
certifikační autorita, a.s.). `DefaultTrust` embeds that chain:

| File | Certificate | Serial | Valid until | SHA-256 of the DER |
| --- | --- | --- | --- | --- |
| `ica-root-ca-rsa-05-2022.pem` | I.CA Root CA/RSA 05/2022 | 100000002 | 2047-05-03 | `d279c01a12e8dd9a6230e459faa447ceb336998477338c2ee4135c96737418eb` |
| `ica-public-ca-rsa-06-2022.pem` | I.CA Public CA/RSA 06/2022 | 100001063 | 2032-06-17 | `df5baf6d7e1a7d14e9911c5b8c676ec6ebcad9354a74f4ac7314e133e07a94de` |

Both files come from I.CA's [certificate page](https://www.ica.cz/korenove-certifikaty),
which publishes the same fingerprints, and are identical to the copies at the
chain's authority information access addresses, `http://s.ica.cz/pca22_rsa.cer`
and `http://r.ica.cz/rca22_rsa.cer`. The Playground's signing certificate
("NEOSTRE - playground prostredi EET") chains to them. The EET operations
documents name the production chain by the two serial numbers above.

To check a file again:

```sh
openssl x509 -in ca/ica-root-ca-rsa-05-2022.pem -noout -fingerprint -sha256
```

These are not the CA EET certificates that issue the merchants' own signing
certificates.
