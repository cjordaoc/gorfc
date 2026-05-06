# Contributing

This repository revives the original `gorfc` project as a community-maintained
Go binding for SAP NetWeaver RFC SDK.

## Ground Rules

- Do not commit SAP NWRFC SDK files. The SDK must be installed separately by
  each user.
- Do not commit SAP credentials, connection strings, SAProuter strings, system
  hostnames, tickets, SNC material, or customer payloads.
- Keep SDK-free tests separate from SAP-backed integration tests.
- Make unsupported behavior explicit. Silent fallback is not acceptable for
  platform, SDK, conversion, ABAP exception, or connection failures.
- Update documentation in the same change when public API, setup, validation,
  or compatibility changes.

## Development Setup

The current upstream code expects SAP NWRFC SDK headers and libraries to be
available to CGO. The revival will replace hardcoded SDK paths with portable
configuration, but until then follow the upstream README and set the relevant
CGO flags for your system.

Expected local checks for documentation-only changes:

```sh
git diff --check
```

Expected local checks for code changes when the SAP SDK is installed:

```sh
gofmt -w .
go test ./...
```

Integration tests must be opt-in and documented before they are required by CI.

## Issue And PR Shape

For fixes:

- describe the observed behavior
- describe the expected behavior
- include platform, Go version, SAP NWRFC SDK version, and SAP system release
  when relevant
- include whether the failure is build-time, connection-time, call-time, or
  conversion-time

For API changes:

- explain why the current API cannot express the behavior safely
- include a migration note
- document compatibility with upstream `gorfc`, `node-rfc`, or PyRFC where
  relevant

