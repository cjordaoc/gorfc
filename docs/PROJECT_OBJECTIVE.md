# Project Objective

`gorfc` is being revived as a community Go connector for the SAP NetWeaver
RFC SDK. It consolidates the best of mature wrappers from other language
ecosystems into a single Go-native library.

The project starts from the existing `SAP-archive/gorfc` codebase rather
than a blank rewrite because the repository already contains useful design
decisions, community feedback, issue history, and working binding code for
core RFC calls. The detailed plan is in [PLAN.md](PLAN.md).

## What This Project Is

- A Go binding over SAP NetWeaver RFC SDK via cgo, isolated behind a
  `Backend` interface so the public API surface contains no `import "C"`.
- A **consolidated superset** of the features offered by SAP JCo,
  `node-rfc`, PyRFC, YaNco, SapNwRfc, and Ruby `nwrfc` — see
  [PLAN.md §3](PLAN.md#3-consolidation-matrix) for the full feature matrix
  (53 features cross-referenced across all six wrappers and the upstream
  `gorfc`).
- A community-maintained path for Go applications that need direct RFC/BAPI
  access in production.
- A modernization effort informed by:
  - **SAP JCo** — protocol coverage reference (sRFC, tRFC, qRFC, bgRFC,
    IDoc, inbound server, repository, all serialization modes, both CPIC
    and WebSocket transports).
  - **`node-rfc`** — API ergonomics reference (cancel, per-call timeout,
    custom file system, pluggable BCD/date/time, WebSocket RFC + TLS).
  - **PyRFC** — error taxonomy reference (`ABAPApplicationError`,
    `ABAPRuntimeError`, `LogonError`, `CommunicationError`,
    `ExternalAuthorizationError`, `ExternalApplicationError`,
    `ExternalRuntimeError`, `RFCError`).
  - **YaNco** (.NET) — runtime/backend abstraction, dependency injection.
  - **SapNwRfc** (.NET) — POCO mapping with attributes, library-presence
    check.
  - **Ruby nwrfc** — BigDecimal preservation, FFI patterns.
  - **upstream `gorfc`** — preserved baseline, type-conversion table, CGO
    bridge structure.
- A library that targets Linux and Windows when the SAP NWRFC SDK is
  installed by the user, with macOS as best-effort.
- Designed to be safe to import in any Go codebase: a build tag
  (`-tags nwrfc_nosdk`) produces a stub build that compiles without the SDK
  present and fails explicitly at runtime with `ErrSDKUnavailable`.

## What This Project Is Not

- It is **not** a pure-Go reimplementation of the proprietary SAP RFC
  protocol. The feasibility analysis (see [PLAN.md §1](PLAN.md#1-executive-summary)
  and §14) concluded this is technically possible but practically infeasible
  for a community project: SAP itself archived PyRFC because no maintainer
  could keep up with the closed SDK; a community effort with worse access
  than SAP itself faces the same sustainability problem. Effort estimates
  for a parity reimplementation run to 50–90 person-months excluding
  ongoing maintenance.
- It does **not** distribute SAP NWRFC SDK, CommonCryptoLib, sapcrypto, or
  any other proprietary SAP artifact. Users obtain them through their own
  SAP entitlement.
- It does **not** bypass SAP authorization, audit, SNC, SAProuter, or
  network policy.
- It is **not** a replacement for OData, SOAP, WebGUI, or Fiori automation
  when those are the correct customer-approved integration paths.
- It is **not** an ORM, query builder, or `database/sql` driver for SAP.

## Differentiation over existing wrappers

Beyond consolidation parity, the Go target adds capabilities that **no
studied wrapper offers**:

- `context.Context` on every blocking operation, with `RfcCancel` wired to
  `ctx.Done()` for true mid-call cancellation.
- A typed error hierarchy (13 categories) supporting `errors.Is/As`,
  `errors.Join`, and `slog.LogValuer` for automatic redaction in logs and
  spans.
- `slog` integration with built-in `RedactHandler` removing PASSWD,
  MYSAPSSO2, x509, SNC partner names, and similar fields automatically.
- OpenTelemetry instrumentation in an opt-in subpackage (`nwrfcotel/`)
  without polluting the core dependency graph.
- Codegen (`cmd/nwrfc-gen`, Tier 4) producing typed BAPI clients from
  `FunctionDescription` queried at design time — no other wrapper offers
  this.
- A pure-Go mock backend (Tier 4) implementing the `Backend` interface,
  enabling SDK-free integration tests for downstream services.
- Build tag `nwrfc_nosdk` letting the library compile in any CI without
  SAP NWRFC SDK present, while remaining explicit about what fails at
  runtime.

## Success Criteria

The revived connector should provide capabilities by tier
(see [PLAN.md §10](PLAN.md#10-roadmap-by-tiers) for full deliverables):

### Tier 0 — Remediation (1–2 PM)

- Committed credentials in `gorfc/sapnwrfc.ini` removed; `.gitignore`
  patterns and gitleaks scanning in place.
- Build tag honest about platform support.
- Memory leaks in legacy `fillVariable` `defer C.free` corrected.
- `docs/SECURITY.md` v0 published.

### Tier 1 — v1.0 baseline (4–6 PM)

- Idiomatic Go API with `context.Context` on every blocking operation.
- Connection lifecycle, pool, explicit stateful sessions.
- All ABAP types with explicit date/time/decimal policy. ABAP `00000000`
  initial date fails explicitly with `ErrZeroDate` instead of silently
  returning `nil` (corrects current upstream behavior; documented as a
  breaking change in the migration guide).
- Typed error hierarchy (13 categories from PyRFC + JCo + node-rfc).
- WebSocket RFC and SNC support, capability-detected against SDK version.
- Build without SDK present (`-tags nwrfc_nosdk`).
- Library-presence check (`EnsureSDK`).
- Clear install instructions for Linux and Windows.

### Tier 2 — v1.0 GA (6–9 PM)

- Throughput tracking via `RfcCreateThroughput`/`RfcGetThroughputXxx`
  (requires SDK 7.53+ 🟡 verify).
- Observability via OpenTelemetry/`slog` opt-in subpackages.
- Synchronous inbound RFC server.
- Custom destination/server providers (analogous to JCo
  `DestinationDataProvider`/`ServerDataProvider`).
- Custom `IniFS` interface for `sapnwrfc.ini` (Kubernetes ConfigMap, S3,
  Vault).
- Full authentication set: password, SNC, X.509, MYSAPSSO2, SAML/Bearer
  🟡 verify against SDK 7.50 PL12+.

### Tier 3 — v1.1 (8–12 PM)

- tRFC, qRFC, bgRFC client + server.
- Repository abstraction (cross-connection metadata cache, pre-load,
  snapshot).
- CLI: `nwrfc ping`, `nwrfc call`, `nwrfc describe`.

### Tier 4 — v1.2/post (6–12 PM)

- Codegen of typed BAPI clients.
- Mock backend for SDK-free integration tests.
- IDoc support (separate package `nwrfcidoc/`).
- Optional HTTP/gRPC bridge.
- Migration guide for users coming from upstream `gorfc`.
- Promotion to `v1.0.0`.

## License Boundary

The repository remains Apache-2.0 as inherited from upstream. SAP NWRFC SDK
and SAP CommonCryptoLib have their own SAP licenses and must be obtained
separately by users with the required SAP entitlement.

**Do not copy SDK artifacts, CommonCryptoLib binaries, vendor headers, or
DLLs into this repository.** This is enforced by `.gitignore` and CI secret
scanning.
