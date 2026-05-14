<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Transversal Audit — v0.2.0

> Captured to satisfy
> [`docs/ROADMAP_NEXUS_INTEGRATION.md` §6](../ROADMAP_NEXUS_INTEGRATION.md#6-transversal-codebase-audit--mandatory-for-v020).
> Run date: 2026-05-08.

This file records the per-area audit findings from the v0.2.0
cycle. Each row carries a verdict and an evidence link or
justification. The audit is mandatory; an empty audit file
would NOT satisfy the roadmap.

## Methodology

For each principle in the roadmap §6 list, we ran a targeted
grep-and-read pass over the tree and recorded:

* **Pattern** — the search.
* **Hits** — what was found.
* **Verdict** — `OK` (clean), `FIXED` (closed during the
  cycle), `KEEP` (intentional, with rationale), or `OPEN`
  (defer with a justification).

## §6.1 OOP idiomático em Go

| Pattern | Hits | Verdict |
|---|---|---|
| Pointer receivers on types holding native handles | `Conn`, `connHandle`, `Pool`, `Transaction`, `Unit` all use pointer receivers consistently. | OK |
| Embedding for hierarchical errors | `LogonError` is embedded by `PasswordExpiredError` / `UserLockedError` / `InvalidCredentialsError` / `UnknownLogonFailureError` (`nwrfc/errors_logon.go`). | OK |
| Small, consumer-side interfaces | `backend.Cancellable`, `backend.Trace`, `backend.LanguageConverter`, `backend.IniReloader`, `backend.CryptoLoader`, `backend.TransactionalBackend`, `backend.BgRFCBackend` are each per-capability. | OK |
| `internal/` boundary respected | All cgo state stays under `internal/sdkbackend`; the public API never returns `unsafe.Pointer`. | OK |

## §6.2 SOLID / DRY / KISS

| Pattern | Hits | Verdict |
|---|---|---|
| Duplicate redaction logic | Pre-v0.2: nwrfcotel and nwrfc/errors.go each had their own. v0.2 routes both through `backend.IsSensitiveKey` (`internal/backend/types.go`). | FIXED |
| Duplicate cancel-watcher goroutine | Pre-v0.2: invoke.go, transaction.go, unit.go each inlined the same select+RfcCancel goroutine. v0.2 extracts to `internal/sdkbackend/cancel.go::withCancelWatcher` + `ctxErrorIfFired`. | FIXED |
| Duplicate RFC_ERROR_GROUP → typed error mapping | One switch in `nwrfc/conn.go::sdkErrorToTyped`. | OK |
| String matching in business code | None outside the declarative `logonClassifications` table. `grep strings.Contains nwrfc/ internal/backend/ -- exclude _test.go and errors_logon.go` returns empty. | OK |
| Duplicate Params validation | Single `Params.validate()`; `enforceCapabilities` adds the runtime gate without duplicating the static checks. | OK |

## §6.3 Segurança RFC/SAP, logs, credenciais

| Pattern | Hits | Verdict |
|---|---|---|
| Password / SSO / bearer / SNC / SAML / X.509 in any log line | Tested via `TestParams_StringerRedacts`, `TestParams_LogValueRedacts`, `TestParams_ExtraRedactedInLogValue`, `TestLogonSubtype_NoSecretLeak`, and `TestErrors_LogValueRedacts`. | OK |
| Trace level capping | Process-global `MaxTraceLevel` cap in `nwrfc/utility.go`; tested by `TestMaxTraceLevel_*`. Unit tests demonstrate `SetTraceLevel(n > cap)` returns `*ConfigError`. | OK |
| Silent fallback when capability missing | WSHost without `Capabilities.WebSocketRFC` fails with `*UnsupportedFeatureError` (`nwrfc/capabilities.go::enforceCapabilities`). Tested by `TestEnforceCapabilities_WSHostHardlock`. | OK |
| Conn.Cancel of mutating ops carries SAP caveat | godoc on `Conn.Cancel` and a section in `docs/ERRORS.md`. | OK |
| Conn.Cancel of mutating ops can produce indeterminate state | Documented as caller responsibility (operation_safety classification in `docs/EVIDENCE/SCHEMA.md`). | OK |

## §6.4 Performance Go

| Pattern | Hits | Verdict |
|---|---|---|
| Regex recompiled per call | None (no regex in nwrfc/ at all; the logon classifier uses `strings.Contains` only). | OK |
| Materialized large tables when streaming would suffice | `RFC_READ_TABLE`-shaped streaming (`iter.Seq`) is **OPEN**. Tracked under post-v0.2 follow-ups; current usage is small fixed-size structs. | OPEN |
| Locks held across SDK calls | The per-Conn mutex is taken inside backend methods only for the duration of the SDK call; the watcher does NOT take it (by design). `Conn.Lock` / `Unlock` are public for the Pool composition path; documented as "will become internal once T1.8 lands". | KEEP |
| Allocation in marshaling hot path | The `inspectStruct` helper caches struct metadata (call.go); `marshalGoValue` allocates per-call but the workload is small fixed structs. Optimization deferred. | OPEN |

## §6.5 Concurrência e lifecycle de handles cgo

| Pattern | Hits | Verdict |
|---|---|---|
| `runtime.SetFinalizer` on `*Conn` | None. The design comment in `nwrfc/conn.go:139-145` calls out the deliberate choice. `grep SetFinalizer nwrfc/ internal/ -- *.go` returns empty. | OK |
| `runtime.SetFinalizer` in upstream `gorfc/gorfc.go` | One occurrence at `gorfc/gorfc.go:1070` for the legacy `*Connection`. The legacy package is the upstream-compat shim per `docs/PORTING_STRATEGY.md`; we keep the original behavior to avoid breaking upstream callers, and the new `nwrfc/Conn` is the finalizer-free path. | KEEP |
| Double-close in nwrfc Conn | Atomic CAS on `connStateOpen → connStateClosed` in `nwrfc/conn.go::Close`. Idempotent; second call returns nil. | OK |
| Use-after-close | `nwrfc/conn.go::checkOpen` rejects with `*BrokenConnectionError`. | OK |
| Cancel after Close | Tested by `TestCancel_AfterCloseIsNoOp`; idempotent no-op. | OK |
| Pool entry openCount race | Mutex-protected in `nwrfc/pool.go`; openCount only changes under `p.mu`. Concurrent load tested by `TestPool_ConcurrentLoad`. | OK |
| Bare `panic` in lifecycle code | Two in `internal/backend/registry.go` for programming-error gates (nil Register, double-registration). Programming bugs only — never reachable at runtime in a correctly built binary. | KEEP |

## §6.6 Build tags, Windows/Linux, VDI

| Pattern | Hits | Verdict |
|---|---|---|
| `cgo && !nwrfc_nosdk` vs `!cgo || nwrfc_nosdk` | All cgo files in `internal/sdkbackend/*.go` carry the first; all stub files in `internal/nosdkbackend/*.go` carry the second. Exactly one backend registers per build (asserted by `backend.Register`'s panic on double-registration). | OK |
| Windows as first-class | `.github/workflows/ci.yml` includes `windows-latest` nosdk build + a Linux→Windows zig cross-compile job. `docs/DEPLOY.md` covers Windows VDI as primary, not best-effort. | OK |
| Hardcoded `C:/Tools/nwrfcsdk` path | `gorfc/gorfc.go` removed in v0.2.0; only roadmap references remain (intentional documentation). | FIXED |
| `EnsureSDK` returns structured errors | `*SDKUnavailableError` with `Reason` and `LookupPath`. Memoized via `sync.OnceValue`. | OK |

## §6.7 Testabilidade determinística

| Pattern | Hits | Verdict |
|---|---|---|
| Cancel/timeout tests use `testing/synctest` | Two synctest tests in `nwrfc/cancel_synctest_test.go` (deadline + immediate cancel). Older non-synctest cancel tests retained as smoke. | OK |
| `time.Sleep` in unit tests where `synctest.Wait` would do | One small sleep in `nwrfc/cancel_test.go::TestCancel_StopsInFlightInvoke` to settle a real-time goroutine; replaced by `synctest.Wait` in the synctest variant. | KEEP |
| `t.Context()` adoption | Not yet rolled out across all tests; current tests use `context.Background()` / `context.WithTimeout`. Follow-up. | OPEN |
| Mock backend coverage | `nwrfcmock` covers every `backend.Backend` method including the new `Cancel`. | OK |

## §6.8 Documentação operacional

| Pattern | Hits | Verdict |
|---|---|---|
| INSTALL, BUILD, DEPLOY, SECURITY, ERRORS, EVIDENCE/SCHEMA, CHANGES, MIGRATION, README consistent with shipped code | All updated in this cycle. CHANGES has v0.2.0 section. ERRORS.md documents logon subtypes + cancellation caveat. SECURITY.md documents the trace cap and the matcher rules. | OK |
| Examples under `example/` compile | `example/pool/main.go` updated for the `Conn.Reset(ctx)` signature change. The other examples did not call Reset and need no edit. | OK |

## Summary

* **FIXED in this cycle:** redaction duplication, cancel-watcher
  duplication, hardcoded SDK paths, missing logon subtypes,
  unguarded WSHost transport, missing trace cap, missing public
  type aliases, missing public Cancel API, missing CI matrix,
  missing VDI deployment guide.
* **OPEN (post-v0.2):** `iter.Seq`-based streaming for large
  tables (§6.4), `t.Context()` rollout (§6.7).
* **KEEP (intentional):** legacy `gorfc/` SetFinalizer (per
  porting strategy), public `Conn.Lock` / `Unlock` until the
  Pool composition is moved internal, programming-error
  panics in `backend.Register`.

The transversal audit is complete for v0.2.0.
