# Certificate authorities

The EET service signs its confirmations with a certificate of the Financial
Administration (O = Generální finanční ředitelství) issued by I.CA (První
certifikační autorita, a.s.). The module embeds that chain:

| File | Certificate | Serial | Valid until | SHA-256 of the DER |
| --- | --- | --- | --- | --- |
| `ica-root-ca-rsa-05-2022.pem` | I.CA Root CA/RSA 05/2022 | 100000002 | 2047-05-03 | `d279c01a12e8dd9a6230e459faa447ceb336998477338c2ee4135c96737418eb` |
| `ica-public-ca-rsa-06-2022.pem` | I.CA Public CA/RSA 06/2022 | 100001063 | 2032-06-17 | `df5baf6d7e1a7d14e9911c5b8c676ec6ebcad9354a74f4ac7314e133e07a94de` |

Both files come from I.CA's [certificate page](https://www.ica.cz/korenove-certifikaty),
which publishes the same fingerprints, and are identical to the copies at the
chain's authority information access addresses, `http://s.ica.cz/pca22_rsa.cer`
and `http://r.ica.cz/rca22_rsa.cer`. The EET operations documents name this
chain by the two serial numbers above, for [production](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_produk-5.pdf#page=7) and the
[Playground](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_playgr-5.pdf#page=7) (v1.2, chapter 3.3), and the service signs its replies as
the [interface description](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_popis_rozhrani_v1.2.pdf#page=37) sets out (v1.2, section 5.3).

## Signing certificates

The certificate that signs the confirmations is not here: the service may
change it at any time while keeping its attributes ("Může se kdykoli změnit
při zachování atributů", [developer seminar slides](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET2_Prezentace_Seminar_pro_vyvojare_23-.pdf#page=44), page 44), so
`ParseResponse` checks the attributes. The operations documents
("Přístupové a provozní informace" v1.2, chapter 3.3, for
[production](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_produk-5.pdf#page=7) and the [Playground](https://eet.gov.cz/assets/cs/cmsmedia/pro-vyvojare/EET_pristupove_provozni_informace_playgr-5.pdf#page=7)) give them:

| Attribute | Production | Playground |
| --- | --- | --- |
| CN | Spolecne technicke zarizeni spravce dane EET | NEOSTRE - playground prostredi EET |
| SERIALNUMBER | ICA - 1275857 | not given, not checked |
| organizationIdentifier | NTRCZ-72080043 | NTRCZ-72080043 |
| O | Generální finanční ředitelství | Generální finanční ředitelství |
| C | CZ | CZ |
| Certificate policy | 1.3.6.1.4.1.23624.10.1.71.1.1 | 1.3.6.1.4.1.23624.10.1.71.1.1 |

To check a file again:

```sh
openssl x509 -in ca/ica-root-ca-rsa-05-2022.pem -noout -fingerprint -sha256
```

These are not the CA EET certificates that issue the merchants' own signing
certificates.
