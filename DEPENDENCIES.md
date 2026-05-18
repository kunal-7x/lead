# OSS Adoption Checklist

For every third-party dependency added to the monorepo, fill out this template
and commit alongside the change.

## Template

```
### <package-name> @ <version>
- Used by: <service or lib>
- Purpose: <why we need it>
- License: <SPDX id> (must be allowed: MIT, Apache-2.0, BSD-2/3, ISC, MPL-2.0)
- Maintenance: last release <date>, contributors <n>, open issues <n>
- Security: known CVEs? <yes/no, refs>
- Alternatives considered: <list>
- Adoption decision: <accept/reject> by <person> on <date>
```

## Disallowed licenses

GPL-2.0, GPL-3.0, AGPL-3.0, SSPL, BUSL, commercial-only, unlicensed.

## Audit cadence

`grype` and `trivy` run in CI on every PR. `syft` SBOM published on main merge.
Quarterly review of this file vs. the lockfiles.

## Phase 24 audit note

No new third-party runtime dependency was added in Phase 24. The security
test harness uses only Go and Python standard libraries. External scanner
tools (`gitleaks`, `trufflehog`, `trivy`, `syft`, `grype`, `zap-baseline`) are
documented in `phases/phase-24/AUDIT_REPORT.md` and must be installed in the
security runner or CI image.
