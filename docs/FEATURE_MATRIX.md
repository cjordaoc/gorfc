<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Feature Matrix

This file is a **navigational index** over the consolidation matrix
in [PLAN.md §3](PLAN.md#3-consolidation-matrix). It groups the 53
features by status (legacy / target / tier), so contributors can
quickly find what is shipping, what is planned, and where each item
will live in code.

The authoritative table — with inspiration sources, SDK functions,
risk, acceptance criteria, and SAP-real-needed flags — is in
[PLAN.md §3](PLAN.md#3-consolidation-matrix). Do not duplicate
content here; update PLAN.md and re-derive this index.

## Legend

- ✅ implemented (in legacy `gorfc/` or in revival packages)
- ✏️ planned, code in progress
- ⏳ planned, not started
- 🔶 partially implemented in legacy `gorfc/`; will be replaced
- 🟡 SDK function or behavior pending verification against the
  NetWeaver RFC SDK Programming Guide (see
  [PLAN.md §3](PLAN.md#3-consolidation-matrix) for the full list)
- 🔴 requires a live SAP system to validate

## Tier 1 — production-grade synchronous client (v0.1.0)

The MVP that lets a Go service call a BAPI in production.

| Group | Features (PLAN.md §3 row #) |
|---|---|
| Connection lifecycle | Direct (#2), load-balanced (#3), `sapnwrfc.ini` destination (#4), `RfcResetServerContext` (#42) |
| Pool & sessions | Pool (#7), explicit stateful Session (#8) |
| Synchronous calls | sRFC client (#1), Cancel via `ctx.Done` (#29), per-call timeout (#30), notRequested (#31), direction filters (#32) |
| Metadata | Function describe (#17), type describe (#18) |
| Marshaling | BCD/decimal (#33), date/time (#34), strict toggles (#35), rstrip (#36), return_import_params (#37), struct tags `rfc:""` (#38) |
| Auth | Password (#22), SNC (#25), CPIC transport (#28), WebSocket RFC (#27) capability-gated, crypto loader (#41) |
| Errors | Typed hierarchy 13 categories (#45) |
| Build & deploy | Library presence check (#46), `nwrfc_nosdk` build tag (#47), idiomatic async via goroutine + `ctx` (#48), runtime/backend abstraction (#49), language ISO/SAP (#43), trace control (#44) |

## Tier 2 — observability, providers, throughput, sync server (v0.2.0)

Brings the library to v1 GA quality with provider-driven config and
opt-in observability.

| Group | Features (PLAN.md §3 row #) |
|---|---|
| Providers | DestinationProvider (#5), ServerProvider (#6) |
| Config sources | Custom IniFS (#39), runtime ini reload (#40) |
| Auth (extended) | SSO ticket MYSAPSSO2 (#23), x509 (#24), SAML/Bearer (#26 — Tier 3 candidate, capability-gated) |
| Server | Synchronous inbound RFC server (#9) |
| Metadata | Cache + invalidate (#19) |
| Performance | Throughput (#21) |
| Observability | OpenTelemetry/`slog` opt-in subpackage `nwrfcotel/` (#52), connection event listeners (#50) |

## Tier 3 — advanced protocols & background server (v0.3.0)

JCo-parity for transactional and queued RFC.

| Group | Features (PLAN.md §3 row #) |
|---|---|
| Transactional client | tRFC client (#10), qRFC client (#12), bgRFC client (#14) |
| Transactional server | tRFC server (#11), qRFC server (#13), bgRFC server (#15) |
| Repository | Cross-connection metadata cache (#20) |
| CLI | `cmd/nwrfc` (ping, call, describe, version) |

## Tier 4 — Go-native differentiation (v1.0.0)

Features no surveyed wrapper offers.

| Group | Features (PLAN.md §3 row #) |
|---|---|
| Code generation | Typed BAPI clients (#51) |
| Test infrastructure | Mock backend (#53) |
| IDoc | `nwrfcidoc/` separate package (#16) |
| Bridge | `cmd/nwrfc-bridge` HTTP/JSON or gRPC façade |
| Migration | `MIGRATION_FROM_GORFC.md` |

## Inspiration crosswalk

For each feature in [PLAN.md §3](PLAN.md#3-consolidation-matrix), the
"Inspiração" column carries one or more letters from this legend:

- **J** — SAP JCo (protocol coverage reference)
- **N** — `node-rfc` (modern API ergonomics)
- **P** — PyRFC (error taxonomy)
- **Y** — YaNco (.NET runtime/backend abstraction)
- **S** — SapNwRfc (.NET POCO mapping, library presence check)
- **R** — Ruby `nwrfc` (FFI patterns, BCD preservation)
- **G** — upstream `gorfc` (CGO bridge, type table)

The Go target consolidates all of these into a single library; no
single source dominates. See
[PORTING_STRATEGY.md](PORTING_STRATEGY.md) for the rationale.

## Verification status legend (🟡 / ✅)

Many SDK functions in the matrix are tagged `🟡` for "requires
verification". This is a deliberate honesty marker — the consolidation
study could not confirm every function name, signature, or minimum SDK
patch level against the SAP NetWeaver RFC SDK Programming Guide
without the SDK in hand. Resolution happens during implementation
PRs (PR description must include the verified primary source for any
🟡 it touches).

`✅ confirmado` markers in [PLAN.md §3](PLAN.md#3-consolidation-matrix)
are functions verified against publicly available SDK documentation
or against a wrapper that demonstrably uses them in production
(`node-rfc`, JCo).

## See also

- [PLAN.md §3](PLAN.md#3-consolidation-matrix) — full matrix with
  acceptance criteria.
- [PLAN.md §10](PLAN.md#10-roadmap-by-tiers) — tier deliverables and
  acceptance criteria.
- [PLAN.md §11](PLAN.md#11-implementation-sequence) — 33-PR sequence
  mapped to features.
- [SDK_FUNCTIONS_MAP.md](SDK_FUNCTIONS_MAP.md) — every SAP NWRFC SDK
  function used, target Go binding location, verification status.
- [ROADMAP.md](ROADMAP.md) — calendar-shaped view of tier delivery.
