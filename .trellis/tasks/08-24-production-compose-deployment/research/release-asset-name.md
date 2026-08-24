# Release asset name normalization

## Confirmed production failure

The `v0.76.0` deployment-assets job uploaded `dist/.env.example`, but the
published GitHub Release exposes that file as `default.env.example`. The release
contains `compose.yaml` and `default.env.example`; it does not contain an asset
named `.env.example`. Consequently, the documented ordinary quick-start URL
`/releases/latest/download/.env.example` returns no usable deployment template.

The workflow itself passed because it validated the local hidden file before
upload. A successful upload job therefore did not prove that the public asset
retained the requested hidden filename.

## Decision

Publish the environment template under the explicit, non-hidden asset name
`compose.env.example`. Operators still download it as the private local file
`.env`:

```bash
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.env.example -o .env
```

The release workflow must stage, validate, and upload exactly
`dist/compose.env.example`. After upload it must query the complete public asset
name list, require exactly one `compose.yaml` and one `compose.env.example`, and
reject `.env.example` plus `default.env.example` without assuming the Release
contains only deployment assets. It must then download both exact public names
to a fresh temporary directory and compare them byte-for-byte with the staged
files before reporting success. Static tests freeze the query, cardinality,
forbidden-name, download, cleanup, comparison, and step-order contracts.
