# gorfc Revival Assessment

## Why Fork Instead Of Restart

The upstream project is old, but it is not disposable. It already includes:

- a CGO binding to `sapnwrfc.h`
- connection open/close and function invocation paths
- stateful and stateless connection concepts
- ABAP-to-Go datatype conversion work
- tests and examples around `STFC_STRUCTURE`
- Apache-2.0 licensing and REUSE metadata
- historical issues and pull requests from real users

The revival should preserve that base, then modernize deliberately.

## Current Strengths

- The package already targets SAP NetWeaver RFC SDK directly.
- Core ABAP datatypes are mapped in `doc/README.md`.
- The code has a real CGO bridge instead of a speculative protocol rewrite.
- The README documents that users must install SAP NWRFC SDK separately.
- The code already models SDK and Go-side errors separately.

## Current Gaps To Validate

- Windows support is documented as incomplete.
- CGO include and library paths are hardcoded for common local SDK locations.
- The public API predates modern Go context/cancellation conventions.
- The module path still points at `github.com/sap/gorfc`.
- Tests appear to require the SAP NWRFC SDK and/or SAP connectivity.
- Credential handling, logging guidance, and integration-test isolation need to
  be tightened before broad community use.
- Error typing should be reviewed against current `node-rfc`, PyRFC, and SAP
  NWRFC SDK behavior.
- Memory ownership across CGO boundaries needs focused review before large
  changes.

## Initial Decision

Keep this fork as the evaluation and revival base. Do not rewrite until the
existing API, issue history, platform gaps, CGO ownership, and node-rfc parity
requirements are mapped.

