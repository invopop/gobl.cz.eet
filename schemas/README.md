# EET schema and WSDL

The message schema and the service description of EET 2.0, from the technical
documentation the Financial Administration publishes on
[Dokumenty k EET 2.0](https://eet.gov.cz/cs/o-eet/dokumenty-k-eet-2-0). Both
files are unedited copies, last checked against that page on 2026-10-15.

| File | Version | SHA-256 |
| --- | --- | --- |
| `EETXMLSchema.xsd` | 4.1 | `83597d08520cbfab7e51145df9a765d5ca22e2f9a38644a468d695657c7a032a` |
| `EETServiceSOAP.wsdl` | 4.1 | `df149c0bef4ac18c2a6e82116b6eb9bb17d29c055cb0821ac6d3885ec90faeb4` |

The tests validate the messages the module builds against the schema. The
WSDL is kept for reference.

To check the files again:

```sh
sha256sum schemas/*.xsd schemas/*.wsdl
```
