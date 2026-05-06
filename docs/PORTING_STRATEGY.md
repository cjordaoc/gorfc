# Porting Strategy

The project goal is not a line-by-line port of `node-rfc`. The goal is a modern
Go connector that reaches similar practical capability while staying idiomatic
for Go and preserving useful upstream `gorfc` behavior.

## Reference Projects

- `SAP-archive/gorfc`: starting point and historical Go implementation.
- `SAP/node-rfc` / `SAP-archive/node-rfc`: API and behavior reference for Node
  users.
- PyRFC: useful reference for install documentation, conversion behavior, and
  common SAP NWRFC SDK expectations.
- SAP NetWeaver RFC SDK: source of truth for C API behavior.

## Phase 1: Baseline And Safety

- Document current build requirements.
- Make tests that do not require SAP SDK run without the SDK.
- Add explicit build tags or stubs for unsupported SDK/platform combinations.
- Remove hardcoded SDK paths from final build configuration.
- Document exact Linux and Windows setup.

## Phase 2: API Modernization

- Introduce context-aware connect and call operations.
- Define typed connection options.
- Define typed SAP SDK/ABAP error categories.
- Keep dynamic maps only where ABAP metadata is genuinely dynamic.
- Preserve migration guidance from upstream `gorfc`.

## Phase 3: Runtime Capability

- Validate `STFC_CONNECTION`.
- Validate `STFC_STRUCTURE`.
- Validate structures, tables, strings, bytes, dates, times, decimals, and
  exceptions.
- Add stateful session tests.
- Add connection lifecycle and pooling guidance.

## Phase 4: node-rfc Parity Review

Compare against `node-rfc` for:

- connection parameters and destination handling
- call result shapes
- metadata lookup
- ABAP exception behavior
- transaction/session handling
- throughput and concurrency expectations
- Unicode and decimal behavior

Document every intentional difference.

## Phase 5: Release Readiness

- Pick a stable module path.
- Add CI for SDK-free checks.
- Add optional integration-test workflow documentation.
- Publish compatibility matrix by Go version, OS, architecture, and SAP NWRFC
  SDK version.
- Tag the first revival release only after install, build, and basic RFC calls
  are independently validated.

