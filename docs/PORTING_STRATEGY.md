# Porting Strategy

> The detailed plan — architecture, feature matrix, modernization plan,
> public API, cgo binding strategy, error taxonomy, security model, testing
> strategy, tiered roadmap, PR sequence, and decision log — lives in
> [PLAN.md](PLAN.md). This document is the high-level overview.

The project goal is **not** a line-by-line port of `node-rfc`, JCo, PyRFC,
or any other single wrapper. The goal is a modern Go connector that
**consolidates the best of all of them** into one Go-native wrapper, while
preserving useful behavior from upstream `gorfc` and its issue history.

## Reference Projects

| Wrapper | Role |
|---|---|
| [`SAP-archive/gorfc`](https://github.com/SAP-archive/gorfc) | Starting point and historical Go implementation. CGO bridge structure preserved; bugs fixed; API replaced. |
| [`SAP/node-rfc`](https://github.com/SAP/node-rfc) | API ergonomics reference: cancel, timeout, custom file system, BCD/date/time pluggable, WebSocket RFC. |
| [`SAP-archive/PyRFC`](https://github.com/SAP-archive/PyRFC) | Error taxonomy reference (8 distinct categories). Install documentation and conversion behavior reference. |
| [SAP JCo](https://support.sap.com/en/product/connectors/jco.html) | Protocol coverage reference: sRFC, tRFC, qRFC, bgRFC, IDoc, inbound server, repository, all serialization modes, both CPIC and WebSocket transports. |
| [`dbosoft/YaNco`](https://github.com/dbosoft/YaNco) (.NET) | Runtime/backend abstraction reference. Dependency-injection-friendly API. |
| [`huysentruitw/SapNwRfc`](https://github.com/huysentruitw/SapNwRfc) (.NET) | POCO/struct mapping with attributes. Library-presence check (`EnsureLibraryPresent`). |
| [`mydoghasworms/nwrfc`](https://github.com/mydoghasworms/nwrfc) (Ruby) | FFI patterns. BigDecimal preservation for BCD types. |
| SAP NetWeaver RFC SDK 7.50 | Source of truth for C API behavior. Validate against the [PL18 (Dec 2025) release](https://userapps.support.sap.com/sap/support/knowledge/en/3302936) programming guide and Doxygen reference. |

## Tier model

The original 5-phase plan in this document is **superseded** by the 4-tier
model in [PLAN.md §10](PLAN.md#10-roadmap-by-tiers).

### Tier 0 — Remediação (1–2 PM)

Pre-requisite. Removes credentials previously committed in
`gorfc/sapnwrfc.ini`, fixes the build tag in `gorfc/gorfc.go:1` that
falsely allowed compilation on Windows where Windows support was
documented as broken, and corrects the cgo memory leaks in
`fillVariable` (lines 187–188) where `defer C.free` is captured before the
pointer is assigned.

### Tier 1 — Cliente sólido (4–6 PM)

Production-grade synchronous RFC client: `Conn`, `Pool`, explicit
stateful sessions, all ABAP types with policy, typed error hierarchy,
`context.Context` cancel via `RfcCancel`, WebSocket RFC capability-detected,
SNC, build-without-SDK via `-tags nwrfc_nosdk`.

### Tier 2 — Observabilidade e server síncrono (6–9 PM)

Throughput tracking, OpenTelemetry/`slog` opt-in subpackages, custom
destination/server providers, custom `IniFS`, synchronous inbound RFC
server, full authentication set (password, SNC, X.509, MYSAPSSO2,
SAML/Bearer).

### Tier 3 — Protocolos avançados (8–12 PM)

tRFC, qRFC, bgRFC client + server. Background RFC handlers (`onCheck`,
`onCommit`, `onConfirm`, `onRollback`, `onGetState`). Repository
abstraction. CLI.

### Tier 4 — Diferenciação Go-native (6–12 PM)

Codegen of typed BAPI clients, pure-Go mock backend, IDoc helpers
(separate package), optional HTTP/gRPC bridge. Promotion to `v1.0.0`.

See [PLAN.md §10](PLAN.md#10-roadmap-by-tiers) for tier-by-tier
deliverables and acceptance criteria, and [§11](PLAN.md#11-implementation-sequence)
for the 33-PR implementation sequence with rollback strategies.

## Architectural decisions captured

The architectural decisions taken during the consolidation analysis are
recorded in [PLAN.md §12](PLAN.md#12-decision-log):

1. IDoc as separate subpackage (Tier 4).
2. WebSocket RFC capability-gated on SDK version, not mandatory.
3. Server-side delivered in Tier 2 (sync) and Tier 3 (tRFC/qRFC/bgRFC).
4. Codegen as `cmd/nwrfc-gen` in the same module (Tier 4).
5. OpenTelemetry as opt-in subpackage `nwrfcotel/`, never in core deps.
6. Mock backend as subpackage `nwrfcmock/` in this repository.
7. macOS as best-effort tier-2.
8. SAP test system: prefer SAP NetWeaver Trial / ABAP Cloud Free Tier
   (🟡 verify availability) plus Docker Developer Edition where licensed.
9. DCO sign-off (not CLA).
10. Go 1.25 minimum (with `toolchain go1.25.0`); `go.mod` line at 1.23.
11. SAP NWRFC SDK 7.50 PL12 minimum; PL18 recommended.
12. `v0.x` allows API iteration; `v1.0` freezes API.

## What is preserved from upstream

- The CGO bridge structure (per-OS `#cgo` directives, helper functions like
  `GoMallocU` / `GoStrlenU`).
- The `wrapVariable` / `wrapStructure` / `wrapTable` family — type-decoding
  logic itself is correct.
- The ABAP-to-Go type mapping table in [doc/README.md](../doc/README.md).
- `wrapConnectionAttributes`, `GetNWRFCLibVersion`,
  `wrapTypeDescription`, `wrapFunctionDescription`.
- Apache-2.0 licensing and REUSE metadata.
- The `rfcSDKError` shape (renamed and partially exposed).

## What is rewritten

- `Connection` → `Conn` with strict lifecycle, mutex, no finalizer for
  handle release.
- `Call` → typed `Call[T]` plus dynamic `CallMap`. `context.Context` first.
- `fillVariable` / `fillStructure` / `fillTable`: bug where `defer C.free`
  is evaluated before pointer assignment is corrected; `mallocU` / `free`
  pairing standardized.
- `ConnectionFromParams` / `ConnectionFromDest` → `Open` / `OpenDest`.
- Errors split into 13-category typed hierarchy ([PLAN.md §7](PLAN.md#7-error-taxonomy)).
- Build flags drop hardcoded SDK paths; configuration via `SAPNWRFC_HOME`
  and `IniFS`.
- ABAP `00000000` initial date no longer returns `nil` silently — explicit
  `ErrZeroDate` unless `AllowZeroDate` option is set.
- Tests separate SDK-free unit tests from SDK-present and SAP-backed
  integration tests.
- Module path migrates from `github.com/sap/gorfc` to a community-owned
  path; a `compat/gorfc` shim package keeps the legacy API available for
  one minor release.
