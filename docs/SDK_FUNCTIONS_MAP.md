<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# SAP NetWeaver RFC SDK Functions Map

This file enumerates every SDK C function the revived `gorfc` calls,
groups them by purpose, and records:

- the Go binding location (file in this repo where the cgo call lives
  — created or planned);
- the tier in which the binding lands;
- the verification status against the SAP NWRFC SDK Programming Guide;
- the minimum SDK patch level (PL) where confirmed.

It is the operational checklist for the bindings the project owes the
SDK. The strategic justification (why these and not others) lives in
[PLAN.md §6](PLAN.md#6-internal-cgo-binding-strategy).

> Verification status:
> - **✅ confirmed** — function exists in the SDK header set the
>   project verified against (7.50 PL12+); Go binding compiles in
>   that environment.
> - **🟡 needs verify** — referenced by PLAN.md or by surveyed
>   wrappers; Go binding either does not exist yet, or exists but
>   has not been validated against the SDK in hand. Implementation
>   PRs MUST replace this marker with ✅ + a source citation
>   (Programming Guide page or commit hash) before merging.
> - **🔶 partial** — binding exists in legacy `gorfc/gorfc.go` but
>   has known bugs (see PLAN.md §1.3 for the audit). Replaced in
>   the appropriate Tier 1 PR.
> - **🔴 not in SDK** — symbol was assumed to exist but is absent
>   from the SDK header + lib set verified against. Bindings that
>   referenced these names have to be reworked against the actual
>   SDK API.

> Verification round 2026-05-07 (NW RFC SDK 7.50 PL18, Linux x86_64,
> build 637): the cgo backend now links cleanly (`go build ./...` +
> `nm -D` on the resulting binaries). 🟡 markers below were flipped
> to ✅ when the symbol is exported by `libsapnwrfc.so` AND the
> Go-side cgo wrapper compiles against `<sapnwrfc.h>`. Behavior
> verification against a live SAP system is a separate gate.

## Initialization & version

| SDK function | Purpose | Go binding (target) | Tier | Status | Min PL |
|---|---|---|---|---|---|
| `RfcGetVersion` | Library version (major, minor, patchlevel) | `internal/sdkbackend/version.go`; legacy `gorfc/gorfc.go:GetNWRFCLibVersion` | T1.5/T1.14 | ✅ | 7.50 PL3 |
| `RfcSetIniPath` | Override ini search path | `internal/sdkbackend/ini.go` | T2.5 | ✅ (PL18) | 7.50 PL3 |
| `RfcReloadIniFile` | Reload after change | `internal/sdkbackend/ini.go` | T2.5 | ✅ (PL18) | 7.50 PL3 |
| `RfcLoadCryptoLibrary` | SAP crypto loader for SNC/TLS | `internal/sdkbackend/crypto.go` | T1.12 | ✅ (PL18) | 7.50 PL10+ 🟡 |
| `RfcSetTraceLevel` / `RfcSetTraceDir` / `RfcSetTraceEncoding` / `RfcSetTraceType` | Trace control | `internal/sdkbackend/trace.go` | T1.13 | ✅ | 7.50 PL3 |
| `RfcLanguageIsoToSap` / `RfcLanguageSapToIso` | Language code conversion | `internal/sdkbackend/lang.go` | T1.13 | ✅ (PL18) | 7.50 PL3 |

## Connection lifecycle

| SDK function | Purpose | Go binding | Tier | Status | Min PL |
|---|---|---|---|---|---|
| `RfcOpenConnection` | Open client connection | `internal/sdkbackend/conn.go:Open`; legacy `ConnectionFromParams` | T1.5 | ✅ | 7.50 PL3 |
| `RfcCloseConnection` | Close | `Conn.Close`; legacy | T1.5 | ✅ | 7.50 PL3 |
| `RfcPing` | Liveness | `Conn.Ping`; legacy `Ping` | T1.5 | ✅ | 7.50 PL3 |
| `RfcIsConnectionHandleValid` | Liveness alternative | `Conn.Alive` | T1.5 | ✅ (PL18) | 7.50 PL3 |
| `RfcGetConnectionAttributes` | Connection metadata | `Conn.Attributes`; legacy `GetConnectionAttributes` | T1.5 | ✅ | 7.50 PL3 |
| `RfcResetServerContext` | ABAP session reset | `Conn.ResetServerContext` | T1.13 | ✅ | 7.50 PL3 |
| `RfcCancel` | Cancel mid-call (cross-thread) | `internal/sdkbackend/cancel.go` (used by ctx watcher in T1.9) | T1.9 | ✅ (PL18) | 7.50 PL3 |

## Function description (metadata)

| SDK function | Purpose | Go binding | Tier | Status |
|---|---|---|---|---|
| `RfcGetFunctionDesc` | Fetch function descriptor (cached by SDK) | `internal/sdkbackend/describe.go`; legacy `GetFunctionDescription` | T1.5 | ✅ |
| `RfcGetParameterCount` | Parameter count | same | T1.5 | ✅ |
| `RfcGetParameterDescByIndex` | Parameter at index | same | T1.5 | ✅ |
| `RfcGetParameterDescByName` | Parameter by name | same | T1.6 | ✅ |
| `RfcGetTypeName` | Structure/table type name | same | T1.7 | ✅ |
| `RfcGetTypeLength` | Type length (UC/NUC) | same | T1.7 | ✅ (PL18) |
| `RfcGetFieldCount` | Field count | same | T1.7 | ✅ |
| `RfcGetFieldDescByIndex` | Field at index | same | T1.7 | ✅ |
| `RfcGetFieldDescByName` | Field by name | same | T1.6 | ✅ |
| `RfcAddFunctionDesc` | Pre-populate metadata cache | `internal/sdkbackend/metadata.go` | T2.6 | ✅ (PL18) |
| `RfcRemoveFunctionDesc` | Invalidate cache entry | same | T2.6 | ✅ (PL18) |

## Function invocation (sRFC)

| SDK function | Purpose | Go binding | Tier | Status |
|---|---|---|---|---|
| `RfcCreateFunction` | Allocate function container | `internal/sdkbackend/invoke.go` | T1.6 | ✅ |
| `RfcInvoke` | Execute RFC call | `internal/sdkbackend/invoke.go:Invoke` | T1.6 | ✅ |
| `RfcDestroyFunction` | Free function container | same | T1.6 | ✅ |
| `RfcSetParameterActive` | notRequested filter | `internal/sdkbackend/invoke.go:applyNotRequested` | T1.6 | ✅ (PL18) |

## Marshaling Go ↔ ABAP scalars

| SDK function | Direction | Go binding |
|---|---|---|
| `RfcSetChars` / `RfcGetChars` | CHAR | `fill.go` / `wrap.go` |
| `RfcSetString` / `RfcGetString` / `RfcGetStringLength` | STRING + BCD/DECF | same |
| `RfcSetNum` / `RfcGetNum` | NUMC | same |
| `RfcSetInt` / `RfcGetInt` | INT, INT2, INT8 | same |
| `RfcSetInt1` / `RfcGetInt1` | INT1 | same |
| `RfcSetInt2` / `RfcGetInt2` | INT2 | same |
| `RfcSetInt8` / `RfcGetInt8` | INT8 | same |
| `RfcSetFloat` / `RfcGetFloat` | FLOAT | same |
| `RfcSetDate` / `RfcGetDate` | DATS | same — handles `00000000` → `ErrZeroDate` (T1.7) |
| `RfcSetTime` / `RfcGetTime` | TIMS | same |
| `RfcSetBytes` / `RfcGetBytes` | BYTE | same |
| `RfcSetXString` / `RfcGetXString` / `RfcGetXStringLength` | XSTRING | same |
| `RfcSetUTCLong` / `RfcGetUTCLong` | UTCLONG (7.50+) | same — 🟡 verify exact symbol vs RfcSetString fallback |

All landing in T1.6/T1.7. Status `🔶` for everything reused from
legacy until the new sdkbackend lands.

## Marshaling structures & tables

| SDK function | Purpose | Go binding | Tier |
|---|---|---|---|
| `RfcGetStructure` | Get nested structure handle | `wrap.go` | T1.7 |
| `RfcSetStructure` | (Not in SDK; use field-level) | n/a | — |
| `RfcGetTable` / `RfcSetTable` | Table reference | `wrap.go` / `fill.go` | T1.7 / T1.6 |
| `RfcAppendNewRow` | Append table row | `fill.go` | T1.6 |
| `RfcMoveTo` / `RfcMoveToFirstRow` / `RfcMoveToNextRow` | Cursor | `wrap.go` | T1.7 |
| `RfcGetRowCount` | Row count | `wrap.go` | T1.7 |

## Encoding helpers

| SDK function | Purpose | Go binding |
|---|---|---|
| `RfcUTF8ToSAPUC` | UTF-8 → SAP_UC | `internal/ucs2/encode.go` (T1.4) |
| `RfcSAPUCToUTF8` | SAP_UC → UTF-8 | `internal/ucs2/decode.go` (T1.4) |

`mallocU` and `freeU` (macros from `<sapuc.h>`) are paired
allocators. The current legacy code uses `C.free` on
`mallocU`-allocated pointers; T1.4 introduces a small static C
helper to use the paired `freeU`. See [PLAN.md §6.2](PLAN.md#62-conversão-sap_uc--utf-8).

## Throughput (T2)

| SDK function | Purpose | Go binding | Status | Min PL |
|---|---|---|---|---|
| `RfcCreateThroughput` | Create stats handle | `nwrfc/throughput.go` (T2.2) | ✅ (PL18) | 7.53+ 🟡 |
| `RfcDestroyThroughput` | Free | same | ✅ (PL18) | 7.53+ 🟡 |
| `RfcSetThroughputOnConnection` | Bind to Conn | same | ✅ (PL18) | 7.53+ 🟡 |
| `RfcRemoveThroughputFromConnection` | Unbind | same | ✅ (PL18) | 7.53+ 🟡 |
| `RfcGetSentBytes` / `RfcGetReceivedBytes` / `RfcGetApplicationTime` / `RfcGetTotalTime` / `RfcGetSerializationTime` / `RfcGetDeserializationTime` / `RfcGetNumberOfCalls` | Stats accessors (note: SDK exports `RfcGetNumberOfCalls`, not `RfcGetCallCount`) | same | ✅ (PL18) | 7.53+ 🟡 |

## Server (T2.7 sync, T3.4 transactional/bg)

| SDK function | Purpose | Go binding | Status |
|---|---|---|---|
| `RfcRegisterServer` | Register at gateway | `nwrfc/server.go` | ✅ (PL18) |
| ~~`RfcStartOrResumeServer`~~ → `RfcStartServer` | Start accept loop. **2026-05-07 finding:** the originally documented name `RfcStartOrResumeServer` does NOT exist in PL18. Real symbol is `RfcStartServer` (sapnwrfc.h:1441). Server accept loops dispatch via `RfcListenAndDispatch` (also exported). | same | 🔴 phantom name → ✅ `RfcStartServer` (PL18) |
| `RfcShutdownServer` | Stop | same | ✅ (PL18) |
| `RfcInstallGenericServerFunction` | Generic dispatcher | same | ✅ (PL18) |
| `RfcInstallServerFunction` | Specific function handler | same | ✅ (PL18) |
| `RfcInstallTransactionHandlers` | tRFC handlers | T3 | ✅ (PL18) |
| `RfcInstallBgRfcHandlers` | bgRFC handlers | T3 | ✅ (PL18) |
| `RfcListenAndDispatch` | Server accept loop primitive | `nwrfc/server.go` | ✅ (PL18) |

## Transactional (T3)

| SDK function | Purpose | Status |
|---|---|---|
| `RfcCreateTransaction` / `RfcInvokeInTransaction` / `RfcSubmitTransaction` / `RfcConfirmTransaction` / `RfcDestroyTransaction` / `RfcGetTransactionID` | tRFC | ✅ all six exported by PL18 (verified 2026-05-07) |
| ~~`RfcSetQueueName`~~ | qRFC queue routing. **2026-05-07 finding:** symbol does NOT exist in PL18 export table or headers. qRFC in NW RFC SDK 7.50 is implemented via `RfcCreateUnit(... queueNames ...)` with the queue list passed as a parameter, not via a separate setter. The `Conn.NewQueuedTransaction(name)` Go API needs to be reimplemented in terms of `RfcCreateUnit`. | 🔴 phantom name (Tier-3 follow-up: rewrite over `RfcCreateUnit`) |
| `RfcCreateUnit` / `RfcInvokeInUnit` / `RfcSubmitUnit` / `RfcConfirmUnit` / `RfcGetUnitState` / `RfcDestroyUnit` | bgRFC (also covers qRFC via queue list arg) | ✅ all six exported by PL18 (verified 2026-05-07) |

## Error info

| SDK function | Purpose | Go binding |
|---|---|---|
| `RFC_ERROR_INFO` (struct, not function) | Error payload from any failed call | `internal/sdkbackend/errors.go:wrapInfo` (T1.3) |

The sdkbackend always passes `&info` to every SDK call and decodes
`info.group`, `info.code`, `info.key`, `info.message`, plus the
`abapMsg{Class,Type,Number,V1..V4}` fields if `group ==
ABAP_APPLICATION_FAILURE` or `ABAP_RUNTIME_FAILURE`. Mapping to the
Go error taxonomy is in [PLAN.md §7](PLAN.md#7-error-taxonomy).

## Headers consumed

The legacy package includes `<sapnwrfc.h>` directly. The new
sdkbackend uses a small per-package shim:

```c
// internal/sdkbackend/helpers.h (per-package shared header)
#include <sapnwrfc.h>
#include <stdlib.h>
SAP_UC*  goMallocU(unsigned size);
void     goFreeU(SAP_UC* p);     // pairs with mallocU; uses standard free()
unsigned goStrlenU(SAP_UC* s);
RFC_RC   goSetParamActive(RFC_FUNCTION_HANDLE h, SAP_UC* name, int active, RFC_ERROR_INFO* info);
```

`mallocU` is a macro that expands to `mallocU16(len)` →
`(SAP_UTF16*) malloc(len * sizeof(SAP_UTF16))` (sapucrfc.h §1869-1870).
**There is no `freeU` macro in the public header set** — pairing
`mallocU` with the standard `free()` is correct on every supported
OS (verified 2026-05-07, NW RFC SDK 7.50 PL18). The wrapper exists
only to keep the malloc/free pairing grep-able in source review.

No other SAP headers are required. `<saptype.h>`, `<sapucrfc.h>`,
and friends are pulled in transitively by `<sapnwrfc.h>`. The
file `<sapuc.h>` referenced by older docs is itself an internal
include shipped under `<sapucrfc.h>` in PL18.

## Phantom symbols — names that don't exist

The doc previously referenced two SDK symbols that do **not**
exist in NW RFC SDK 7.50 PL18 (verified by `nm -D` over the
exported table on 2026-05-07):

| Reference | Reality |
|---|---|
| `RfcStartOrResumeServer` | The real symbol is `RfcStartServer` (sapnwrfc.h:1441). Server accept loops use `RfcListenAndDispatch`. |
| `RfcSetQueueName` | qRFC queue routing is done by passing the queue list as a parameter to `RfcCreateUnit`, not via a separate setter. |

Both names appeared in older PLAN.md drafts and may surface in
future contributions copied from non-SAP wrappers. They are
NOT valid SDK API and binding code that names them will fail
at link time.

## See also

- [PLAN.md §6](PLAN.md#6-internal-cgo-binding-strategy) — binding
  strategy with handle ownership and memory-safety rules.
- [PLAN.md §7](PLAN.md#7-error-taxonomy) — error decode and
  category mapping.
- [PLAN.md §10](PLAN.md#10-roadmap-by-tiers) — tier deliverables.
- [SAP NetWeaver RFC SDK Programming Guide](https://support.sap.com/en/product/connectors/nwrfcsdk.html)
  — primary source for all `RfcXxx` calls.
