# OpenAPI

The dashboard/BFF OpenAPI source lives at `../../apps/dashboard/openapi.yaml`.

Lint command:

```bash
npx @redocly/cli lint apps/dashboard/openapi.yaml
```

Static docs command:

```bash
npx @redocly/cli build-docs apps/dashboard/openapi.yaml -o docs/api/openapi.html
```
