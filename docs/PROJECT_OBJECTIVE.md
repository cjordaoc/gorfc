# Project Objective

`gorfc` is being revived as a community Go connector for SAP NetWeaver RFC SDK.

The project starts from the existing `SAP-archive/gorfc` codebase instead of a
blank rewrite because the repository already contains useful design decisions,
community feedback, issue history, and working binding code for core RFC calls.

## What This Project Is

- A Go binding over SAP NetWeaver RFC SDK.
- A community-maintained path for Go applications that need direct RFC/BAPI
  access.
- A modernization effort informed by `node-rfc`, PyRFC, and the historical
  `gorfc` implementation.
- A library that should work cleanly on Linux and Windows when the SAP NWRFC SDK
  is installed by the user.

## What This Project Is Not

- It is not a pure-Go reimplementation of the proprietary SAP RFC protocol.
- It does not distribute SAP NWRFC SDK.
- It does not bypass SAP authorization, audit, SNC, SAProuter, or network
  policy.
- It is not a replacement for OData, SOAP, WebGUI, or Fiori automation when
  those are the correct customer-approved integration paths.

## Success Criteria

The revived connector should provide:

- clear install instructions for Linux and Windows
- explicit SAP SDK discovery and build configuration
- idiomatic Go API with context-aware calls
- stateless and stateful RFC sessions
- safe connection lifecycle and pooling guidance
- deterministic ABAP type conversion
- structured SAP SDK and ABAP error surfaces
- opt-in integration tests using standard SAP RFC test functions
- compatibility notes for users migrating from upstream `gorfc` or `node-rfc`

## License Boundary

The repository remains Apache-2.0 as inherited from upstream. SAP NWRFC SDK has
its own SAP license and must be obtained separately by users with the required
SAP entitlement. Do not copy SDK artifacts into this repository.

