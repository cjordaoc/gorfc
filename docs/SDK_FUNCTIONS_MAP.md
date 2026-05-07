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

## Initialization & version

| SDK function | Purpose | Go binding (target) | Tier | Status | Min PL |
|---|---|---|---|---|---|
| `RfcGetVersion` | Library version (major, minor, patchlevel) | `internal/sdkbackend/version.go`; legacy `gorfc/gorfc.go:GetNWRFCLibVersion` | T1.5/T1.14 | ✅ | 7.50 PL3 |
| `RfcSetIniPath` | Override ini search path | `internal/sdkbackend/ini.go` | T2.5 | 🟡 | 7.50 PL3 |
| `RfcReloadIniFile` | Reload after change | `internal/sdkbackend/ini.go` | T2.5 | 🟡 | 7.50 PL3 |
| `RfcLoadCryptoLibrary` | SAP crypto loader for SNC/TLS | `internal/sdkbackend/crypto.go` | T1.12 | 🟡 | 7.50 PL10+ 🟡 |
| `RfcSetTraceLevel` / `RfcSetTraceDir` / `RfcSetTraceEncoding` / `RfcSetTraceType` | Trace control | `internal/sdkbackend/trace.go` | T1.13 | ✅ | 7.50 PL3 |
| `RfcLanguageIsoToSap` / `RfcLanguageSapToIso` | Language code conversion | `internal/sdkbackend/lang.go` | T1.13 | 🟡 | 7.50 PL3 |

## Connection lifecycle

| SDK function | Purpose | Go binding | Tier | Status | Min PL |
|---|---|---|---|---|---|
| `RfcOpenConnection` | Open client connection | `internal/sdkbackend/conn.go:Open`; legacy `ConnectionFromParams` | T1.5 | ✅ | 7.50 PL3 |
| `RfcCloseConnection` | Close | `Conn.Close`; legacy | T1.5 | ✅ | 7.50 PL3 |
| `RfcPing` | Liveness | `Conn.Ping`; legacy `Ping` | T1.5 | ✅ | 7.50 PL3 |
| `RfcIsConnectionHandleValid` | Liveness alternative | `Conn.Alive` | T1.5 | 🟡 | 7.50 PL3 |
| `RfcGetConnectionAttributes` | Connection metadata | `Conn.Attributes`; legacy `GetConnectionAttributes` | T1.5 | ✅ | 7.50 PL3 |
| `RfcResetServerContext` | ABAP session reset | `Conn.ResetServerContext` | T1.13 | ✅ | 7.50 PL3 |
| `RfcCancel` | Cancel mid-call (cross-thread) | `internal/sdkbackend/cancel.go` (used by ctx watcher in T1.9) | T1.9 | 🟡 | 7.50 PL3 |

## Function description (metadata)

| SDK function | Purpose | Go binding | Tier | Status |
|---|---|---|---|---|
| `RfcGetFunctionDesc` | Fetch function descriptor (cached by SDK) | `internal/sdkbackend/describe.go`; legacy `GetFunctionDescription` | T1.5 | ✅ |
| `RfcGetParameterCount` | Parameter count | same | T1.5 | ✅ |
| `RfcGetParameterDescByIndex` | Parameter at index | same | T1.5 | ✅ |
| `RfcGetParameterDescByName` | Parameter by name | same | T1.6 | ✅ |
| `RfcGetTypeName` | Structure/table type name | same | T1.7 | ✅ |
| `RfcGetTypeLength` | Type length (UC/NUC) | same | T1.7 | 🟡 |
| `RfcGetFieldCount` | Field count | same | T1.7 | ✅ |
| `RfcGetFieldDescByIndex` | Field at index | same | T1.7 | ✅ |
| `RfcGetFieldDescByName` | Field by name | same | T1.6 | ✅ |
| `RfcAddFunctionDesc` | Pre-populate metadata cache | `internal/sdkbackend/metadata.go` | T2.6 | 🟡 |
| `RfcRemoveFunctionDesc` | Invalidate cache entry | same | T2.6 | 🟡 |

## Function invocation (sRFC)

| SDK function | Purpose | Go binding | Tier | Status |
|---|---|---|---|---|
| `RfcCreateFunction` | Allocate function container | `internal/sdkbackend/invoke.go` | T1.6 | ✅ |
| `RfcInvoke` | Execute RFC call | `internal/sdkbackend/invoke.go:Invoke` | T1.6 | ✅ |
| `RfcDestroyFunction` | Free function container | same | T1.6 | ✅ |
| `RfcSetParameterActive` | notRequested filter | `internal/sdkbackend/fill.go` | T1.6 | 🟡 |

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
| `RfcCreateThroughput` | Create stats handle | `nwrfc/throughput.go` (T2.2) | 🟡 | 7.53+ 🟡 |
| `RfcDestroyThroughput` | Free | same | 🟡 | 7.53+ 🟡 |
| `RfcSetThroughputOnConnection` | Bind to Conn | same | 🟡 | 7.53+ 🟡 |
| `RfcRemoveThroughputFromConnection` | Unbind | same | 🟡 | 7.53+ 🟡 |
| `RfcGetCallCount` / `RfcGetSentBytes` / `RfcGetReceivedBytes` / `RfcGetApplicationTime` / `RfcGetTotalTime` / `RfcGetSerializationTime` / `RfcGetDeserializationTime` | Stats accessors | same | 🟡 | 7.53+ 🟡 |

## Server (T2.7 sync, T3.4 transactional/bg)

| SDK function | Purpose | Go binding | Status |
|---|---|---|---|
| `RfcRegisterServer` | Register at gateway | `nwrfc/server.go` | 🟡 |
| `RfcStartOrResumeServer` | Start accept loop | same | 🟡 |
| `RfcShutdownServer` | Stop | same | 🟡 |
| `RfcInstallGenericServerFunction` | Generic dispatcher | same | 🟡 |
| `RfcInstallServerFunction` | Specific function handler | same | 🟡 |
| `RfcInstallTransactionHandlers` | tRFC handlers | T3 | 🟡 |
| `RfcInstallBgRfcHandlers` | bgRFC handlers | T3 | 🟡 |

## Transactional (T3)

| SDK function | Purpose | Status |
|---|---|---|
| `RfcCreateTransaction` / `RfcInvokeInTransaction` / `RfcSubmitTransaction` / `RfcConfirmTransaction` / `RfcDestroyTransaction` / `RfcGetTransactionID` | tRFC | 🟡 |
| `RfcSetQueueName` | qRFC | 🟡 |
| `RfcCreateUnit` / `RfcInvokeInUnit` / `RfcSubmitUnit` / `RfcConfirmUnit` / `RfcGetUnitState` / `RfcDestroyUnit` | bgRFC | ✅ confirmed in node-rfc bindings |

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
sdkbackend will include only:

```c
#include <sapnwrfc.h>
#include <sapuc.h>      // for SAP_UC, mallocU, freeU
```

No other SAP headers are required. `<saptype.h>`, `<sapucum.h>`,
and friends are pulled in transitively by `<sapnwrfc.h>` and
`<sapuc.h>`.

## See also

- [PLAN.md §6](PLAN.md#6-internal-cgo-binding-strategy) — binding
  strategy with handle ownership and memory-safety rules.
- [PLAN.md §7](PLAN.md#7-error-taxonomy) — error decode and
  category mapping.
- [PLAN.md §10](PLAN.md#10-roadmap-by-tiers) — tier deliverables.
- [SAP NetWeaver RFC SDK Programming Guide](https://support.sap.com/en/product/connectors/nwrfcsdk.html)
  — primary source for all `RfcXxx` calls.
