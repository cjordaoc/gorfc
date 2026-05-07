<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Roadmap

This file is a **navigational index** over the tier-based plan in
[PLAN.md §10](PLAN.md#10-roadmap-by-tiers) and the 33-PR sequence
in [PLAN.md §11](PLAN.md#11-implementation-sequence). It is the
short, scannable answer to "where is the project, and what comes
next?".

The authoritative tier scope, acceptance criteria, and PR list live
in PLAN.md. Update PLAN.md and re-derive this file.

## Snapshot

| Tier | Target version | State | What it delivers |
|---|---|---|---|
| **T0** | (no release) | 🟢 in progress | Security remediation: credentials purged, build tag honest, memory leak fixed, secret-scan workflow, `docs/SECURITY.md` v0. |
| **T1** | v0.1.0 | ⏳ planned | Production-grade synchronous client: `Conn`, `Pool`, `Session`, ABAP types, typed errors, `ctx.Cancel`, WebSocket RFC capability-gated, SNC, `nwrfc_nosdk` build. |
| **T2** | v0.2.0 | ⏳ planned | Throughput, OpenTelemetry/`slog` opt-in, custom destination/server providers, custom `IniFS`, sync inbound RFC server, full auth set. |
| **T3** | v0.3.0 | ⏳ planned | tRFC, qRFC, bgRFC client + server, repository abstraction, `cmd/nwrfc` CLI. |
| **T4** | v1.0.0 | ⏳ planned | Codegen, mock backend, `nwrfcidoc/`, `cmd/nwrfc-bridge`, migration guide, GA. |

## Tier 0 — security remediation (✅ in this revival sprint)

Status: implementing now. Goal: zero active AGENTS.md violations
before any new feature work begins.

PRs (PLAN.md §11 rows T0.1–T0.4):

- T0.1 — credentials purged, `.gitignore` patterns, `docs/SECURITY.md` v0,
  GHA `secret-scan.yml`. ✅
- T0.2 — build tag tightened to explicit per-GOOS list. ✅
- T0.3 — `defer C.free` capture-before-assignment leak in
  `fillVariable` fixed. ✅
- T0.4 — docs/FEATURE_MATRIX.md, docs/SDK_FUNCTIONS_MAP.md,
  docs/ROADMAP.md (this file). ✅

Acceptance: see [PLAN.md §10 Tier 0](PLAN.md#tier-0--remediação-imediata-12-pm).

No release tag. Tier 0 lands on `master` directly; Tier 1 work
starts on top of it.

## Tier 1 — production-grade synchronous client (v0.1.0)

The minimum viable wrapper that lets a Go service call any sRFC /
BAPI in production with proper context cancellation, typed errors,
and observability hooks.

What lands:

- `internal/backend` — backend interface (T1.1).
- `nwrfc/` — public package: `Conn`, `Pool`, `Session`, `Params`,
  `Call[T]` + `CallMap` + tag parser (T1.2 / T1.8 / T1.10 / T1.11).
- `internal/ucs2`, `internal/bcd`, `internal/timeext` — pure-Go
  ABAP-type utilities (T1.4).
- `internal/sdkbackend` — cgo bindings for Open/Close/Ping/Describe/
  Invoke/Cancel/Attributes/ResetServerContext (T1.5–T1.7, T1.9).
- `internal/nosdkbackend` — stub for `nwrfc_nosdk` build (T1.14).
- `nwrfc/errors.go` — 13-category typed hierarchy with redaction
  (T1.3).
- WebSocket RFC + crypto loader, capability-gated against SDK
  version (T1.12).
- Trace control, ini reload, language ISO/SAP (T1.13).
- `EnsureSDK()` library presence check (T1.14).
- Documentation: INSTALL, BUILD, CONFIGURATION, ERRORS (T1.15).
- Examples: `ping`, `stfc_structure`, `pool`, `session` (T1.15).
- `CHANGES` entry + version bump (T1.16).

Acceptance: see [PLAN.md §10 Tier 1](PLAN.md#tier-1--cliente-sólido-para-produção-46-pm).

Out of scope (deferred to T2): server runtime, throughput,
OpenTelemetry, providers.

## Tier 2 — observability, providers, throughput, sync server (v0.2.0)

Goes from MVP to GA-grade.

What lands:

- `nwrfcparam.BAPIRet2` helper (T2.1).
- Throughput SDK bindings (T2.2; SDK 7.53+ 🟡).
- `nwrfcotel/` opt-in subpackage with redacting `slog` handler (T2.3).
- `DestinationProvider` and `ServerProvider` interfaces (T2.4).
- Custom `IniFS` (T2.5).
- Metadata cache + `Invalidate` (T2.6).
- Synchronous inbound RFC server (T2.7).
- Auth set: SSO ticket, x509, SAML/Bearer (T2.8).
- Connection event listeners (T2.9).
- `CHANGES` for v0.2.0 (T2.10).

Acceptance: see [PLAN.md §10 Tier 2](PLAN.md#tier-2--observabilidade-providers-throughput-server-síncrono-69-pm).

Out of scope (deferred to T3): tRFC/qRFC/bgRFC, IDoc, codegen.

## Tier 3 — advanced protocols and background server (v0.3.0)

Reaches JCo parity for transactional and queued RFC.

What lands:

- tRFC client + server (T3.1, T3.4).
- qRFC client + server (T3.2, T3.4).
- bgRFC client + server (T3.3, T3.4).
- `nwrfc.Repository` abstraction (T3.5).
- `cmd/nwrfc` CLI (T3.6).
- `CHANGES` for v0.3.0 (T3.7).

Acceptance: see [PLAN.md §10 Tier 3](PLAN.md#tier-3--protocolos-avançados-e-server-bgtrans-812-pm).

Risk concentration: bgRFC server callbacks. PyRFC archived in part
because of unmaintainable bgRFC server complexity. Mitigation: more
SAP integration testing, smaller PRs, clear escape hatches.

## Tier 4 — Go-native differentiation (v1.0.0)

Features that no surveyed wrapper offers, plus the GA tag.

What lands:

- `nwrfcmock/` — pure-Go mock implementing the `Backend`
  interface (T4.1).
- `cmd/nwrfc-gen` — codegen of typed BAPI client packages (T4.2).
- `nwrfcidoc/` — IDoc parser/builder, no SAP-cgo (T4.3).
- `cmd/nwrfc-bridge` — HTTP/JSON or gRPC façade (T4.4).
- `docs/MIGRATION_FROM_GORFC.md` (T4.5).
- `CHANGES` for v1.0.0 (T4.6).

Acceptance: see [PLAN.md §10 Tier 4](PLAN.md#tier-4--diferenciação-go-native-612-pm).

After v1.0.0, the public API freezes per
[PLAN.md §12 decision 12](PLAN.md#12-decision-log).

## Effort estimates (informative)

The plan's tier estimates are senior-engineer person-months in a
SAP-context-aware shop. They are not deadlines; gorfc is a
volunteer community project. Estimates are useful for sequencing,
not for promising calendar dates.

| Tier | Est. PM | Notes |
|---|---|---|
| T0 | 1–2 | Mostly mechanical; AI-assisted execution compresses to days. |
| T1 | 4–6 | The bulk of binding code. Compresses some with AI but SAP integration testing dominates. |
| T2 | 6–9 | Server runtime + observability are the long poles. |
| T3 | 8–12 | bgRFC is the hard one. SAP test infra is the rate limit. |
| T4 | 6–12 | Codegen and IDoc are independent — can parallelize. |

## See also

- [PLAN.md §10](PLAN.md#10-roadmap-by-tiers) — tier deliverables and
  acceptance criteria.
- [PLAN.md §11](PLAN.md#11-implementation-sequence) — every PR with
  rollback strategy.
- [FEATURE_MATRIX.md](FEATURE_MATRIX.md) — feature → tier mapping.
- [SDK_FUNCTIONS_MAP.md](SDK_FUNCTIONS_MAP.md) — SDK function → Go
  binding location.
- [SECURITY.md](SECURITY.md) — non-negotiable security policy.
- [PROJECT_OBJECTIVE.md](PROJECT_OBJECTIVE.md) — scope and what this
  project is *not*.
