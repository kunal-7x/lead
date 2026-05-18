# API Docs

This directory contains static API documentation entry points.

- BFF OpenAPI source: `../../apps/dashboard/openapi.yaml`
- Protobuf source: `../../proto`
- Redoc command: `npx @redocly/cli build-docs ../../apps/dashboard/openapi.yaml -o openapi.html`
- Buf docs command: `buf generate --template ../../buf.gen.docs.yaml`

Generated HTML and protobuf docs should be published as a static site by CI.
