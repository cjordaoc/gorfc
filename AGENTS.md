# AGENTS.md

## Scope

This document governs coding assistants working in this repository. It applies
to AI systems that read, write, refactor, test, review, document, or publish
changes here.

The repository is a community revival of `SAP-archive/gorfc`, a Go binding for
SAP NetWeaver RFC SDK. The objective is to modernize the connector while
preserving upstream history and community feedback.

Read first:

- `docs/PROJECT_OBJECTIVE.md`
- `docs/GORFC_REVIVAL_ASSESSMENT.md`
- `docs/PORTING_STRATEGY.md`
- `README.md`

## Non-Negotiables

- Do not vendor, commit, embed, or redistribute SAP NWRFC SDK binaries, headers,
  DLLs, shared libraries, installers, credentials, or customer-specific SAP
  system details.
- Do not claim pure-Go RFC support unless the implementation actually avoids
  SAP NWRFC SDK and any proprietary protocol dependency.
- Do not fake live SAP validation. A successful build is not proof of RFC
  behavior.
- Do not hide partial compatibility with `node-rfc`, PyRFC, or upstream `gorfc`.
- Do not introduce silent fallback behavior. Missing SDK, missing headers,
  missing libraries, unsupported platform, RFC errors, ABAP exceptions, and
  conversion failures must fail explicitly.
- Do not hardcode local SDK paths as the final design. Build configuration must
  be documented and portable.
- Do not add global mutable state for connections, destinations, credentials,
  metadata caches, or SDK handles unless ownership and concurrency rules are
  documented and tested.
- Do not log passwords, SAP tickets, SNC material, connection strings with
  secrets, or payloads that may contain business-sensitive data.

## Engineering Rules

- Prefer small typed APIs over `map[string]interface{}` at public boundaries
  when the shape is known. Dynamic ABAP structures may use dynamic maps, but the
  RFC call, connection, metadata, error, and transaction APIs should be typed.
- `context.Context` is the first parameter for operations that connect, invoke
  RFCs, block, or may need cancellation.
- Errors are values. Preserve SAP SDK and ABAP error detail while wrapping with
  Go error chains.
- Every connection has a clear owner and close path. Stateful ABAP contexts must
  be explicit.
- Shared state must be guarded by a mutex, owned by a goroutine, or immutable.
- Tests must separate SDK-free unit tests from SAP-backed integration tests.
- Integration tests must be opt-in through documented environment variables and
  must never require committed credentials.
- Public API changes require README and docs updates in the same change.
- Keep compatibility notes explicit: upstream `gorfc`, `node-rfc`, PyRFC, and
  SAP NWRFC SDK behavior should not be described from memory when it can be
  verified from source or vendor docs.

## Required Workflow

Before coding:

1. Inspect the current upstream code and affected package.
2. Check whether an issue, old PR, or upstream behavior already covers the
   topic.
3. Identify whether the change is documentation-only, SDK-binding behavior,
   API shape, build system, or integration validation.
4. For SAP NWRFC SDK, `node-rfc`, PyRFC, Go toolchain, platform, security, or
   licensing claims, verify current primary sources before documenting them.

Before writing binding code:

1. Confirm the SDK ownership and lifetime model.
2. Confirm memory allocation/freeing for every C value crossing the CGO
   boundary.
3. Confirm Unicode, decimal, date/time, table, structure, and exception
   behavior against SDK docs or a live integration fixture.
4. Confirm the design works on Windows and Linux, or document the unsupported
   platform explicitly.

After coding:

1. Run `gofmt`.
2. Run SDK-free tests.
3. Run integration tests only when the SAP NWRFC SDK and a test SAP system are
   configured.
4. Document skipped validation with the exact blocker.
5. Update docs when behavior, API, build requirements, or compatibility changes.

## Definition Of Done

A task is not done if:

- SDK artifacts or credentials were committed
- a platform is claimed without build or runtime evidence
- an RFC call path returns synthetic success
- tests require private SAP access without an opt-in guard
- docs describe commands, env vars, or behavior that do not exist
- a known compatibility gap is hidden instead of documented
- new CGO memory ownership is untested or unexplained

## Current Priorities

1. Preserve upstream behavior and document the existing gap.
2. Make build configuration portable across Linux and Windows.
3. Introduce SDK-free tests for conversion/error helpers where possible.
4. Add opt-in SAP integration tests for `STFC_CONNECTION` and
   `STFC_STRUCTURE`.
5. Define a modern API layer compatible with Go contexts and explicit errors.

