<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Error Hierarchy

A typed Go error hierarchy over the SAP NetWeaver RFC SDK error
groups. Implements docs/PLAN.md §7.

## At-a-glance

```
RFCError (interface)                  Category()
├── LogonError                        CategoryLogon
├── CommunicationError                CategoryCommunication
├── ABAPApplicationError              CategoryABAPApp
├── ABAPRuntimeError                  CategoryABAPRuntime
├── ABAPClassicException              CategoryABAPClassic
├── ABAPClassException                CategoryABAPClass
├── ExternalAuthorizationError        CategoryExtAuthz
├── ExternalApplicationError          CategoryExtApp
├── ExternalRuntimeError              CategoryExtRuntime
├── BrokenConnectionError             CategoryBrokenConn
├── TimeoutError                      CategoryTimeout
├── CancelledError                    CategoryCancelled
├── MarshalError                      CategoryMarshal
├── ConfigError                       CategoryConfig
├── SDKUnavailableError               CategorySDKUnavailable
└── UnsupportedFeatureError           CategoryUnsupported
```

## Sentinels

```go
var (
    ErrLogon            = ...   // category sentinel
    ErrCommunication    = ...
    ErrABAPApplication  = ...
    ErrABAPRuntime      = ...
    ErrABAPClassic      = ...
    ErrABAPClass        = ...
    ErrExtAuthz         = ...
    ErrExtApp           = ...
    ErrExtRuntime       = ...
    ErrBrokenConn       = ...
    ErrTimeout          = ...
    ErrCancelled        = ...
    ErrMarshal          = ...
    ErrConfig           = ...
    ErrSDKUnavailable   = ...
    ErrUnsupported      = ...

    // Domain-specific sentinels (refine MarshalError)
    ErrZeroDate    = ...   // ABAP "00000000"
    ErrZeroTime    = ...   // ABAP "000000"
    ErrUnknownType = ...
    ErrConnClosed  = ...
)
```

## Branching

```go
// Quick categorization:
switch nwrfc.CategoryOf(err) {
case backend.CategoryLogon:        // bad password, locked user, ...
case backend.CategoryCommunication: // network / gateway issue
case backend.CategoryABAPApp:       // RFM raised RAISE
case backend.CategoryABAPRuntime:   // ABAP short dump
case backend.CategoryTimeout:       // ctx deadline elapsed
case backend.CategoryCancelled:     // ctx cancelled
}

// Sentinel match:
if errors.Is(err, nwrfc.ErrLogon) { /* re-prompt for password */ }

// Concrete struct:
var abap *nwrfc.ABAPApplicationError
if errors.As(err, &abap) {
    log.Printf("ABAP error %s/%s number=%s",
        abap.AbapMsgClass, abap.AbapMsgType, abap.AbapMsgNumber)
}
```

## Retryability

```go
if nwrfc.IsRetryable(err) {
    // Communication / Broken / Timeout — retry against fresh Conn.
}
```

`IsRetryable` returns true ONLY for transport-shaped failures.
Logon/marshal/config/abap errors are deterministic — retrying without
fixing the input wastes round-trips.

Inside a Pool:

```go
err := pool.Do(ctx, func(c *nwrfc.Conn) error {
    _, err := nwrfc.Call(ctx, c, fn, in, &out)
    return err
})
if nwrfc.IsRetryable(err) {
    // try once more
    err = pool.Do(ctx, ...)
}
```

The Pool already discards Conns that errored, so the next Acquire
opens a fresh connection.

## Redaction

Every error implements `slog.LogValuer` with redaction baked in.
Logging an error never leaks credentials or business data:

```go
slog.Error("rfc call failed", "err", err)
// JSON output redacts AbapMsgV1..V4, ExternalAuthorizationError full
// message, sensitive ConfigError hints.
```

The fields that ARE emitted (Code, Group, Key, top-level Message,
Function, AbapMsgClass/Type/Number) are safe by design — they identify
the failure shape, not the payload.

## Domain sentinels

`ErrZeroDate` and `ErrZeroTime` close the upstream silent-zero-time
bug from PLAN.md §1.3. Default behavior: an ABAP "00000000" or
"000000" returned by the SDK fails the unmarshal with a
`*MarshalError` wrapping the sentinel. Opt out per-call:

```go
nwrfc.Call(ctx, conn, fn, in, &out, nwrfc.CallOptions{
    AllowZeroDate: true,
    AllowZeroTime: true,
})
```

## Mapping from RFC_ERROR_INFO

The cgo backend decodes `RFC_ERROR_INFO` and emits a
`*backend.SDKError` carrying the `Group`. The public package's
`mapBackendError` switches on Group to produce the typed struct:

| `RFC_ERROR_GROUP` | Typed struct |
|---|---|
| `LOGON_FAILURE` | `*LogonError` |
| `COMMUNICATION_FAILURE` | `*CommunicationError` |
| `ABAP_APPLICATION_FAILURE` | `*ABAPApplicationError` |
| `ABAP_RUNTIME_FAILURE` | `*ABAPRuntimeError` |
| `EXTERNAL_AUTHORIZATION_FAILURE` | `*ExternalAuthorizationError` |
| `EXTERNAL_APPLICATION_FAILURE` | `*ExternalApplicationError` |
| `EXTERNAL_RUNTIME_FAILURE` | `*ExternalRuntimeError` |

`ABAP_EXCEPTION` (the classic / class-based split) maps to
`*ABAPClassicException` or `*ABAPClassException` based on whether
the SDK reported a class name; in PL12+ the SDK populates ClassName
for class-based exceptions only.

## Logon subtypes (v0.2.0+)

A `LogonError` from the SDK is further classified by the
library into one of four subtypes. Generic callers using
`errors.Is(err, ErrLogon)` keep working unchanged; specific
controllers can branch on the subtype to drive UX (re-prompt,
"reset password", "open SAP ticket"):

| Subtype | Sentinel | Operational meaning |
|---|---|---|
| `*PasswordExpiredError` | `ErrPasswordExpired` | User must reset the password (transaction SU01 or self-service). Re-prompting will not succeed. |
| `*UserLockedError` | `ErrUserLocked` | Admin must unlock (SU01). Retrying may extend the lockout. |
| `*InvalidCredentialsError` | `ErrInvalidCredentials` | Wrong user or password. Re-prompt the human; never log the value. |
| `*UnknownLogonFailureError` | `ErrUnknownLogonFailure` | The classifier could not match the SDK key/message. Stay in the logon category — do NOT fall through to communication. |

The classifier is declarative (`nwrfc/errors_logon.go::logonClassifications`).
To extend coverage for a new SAP message phrase, add a row;
do not scatter `strings.Contains` checks into business code.

```go
err := nwrfc.Open(ctx, p)
switch {
case errors.Is(err, nwrfc.ErrPasswordExpired):
    // surface "your password expired" UX
case errors.Is(err, nwrfc.ErrUserLocked):
    // escalate to admin
case errors.Is(err, nwrfc.ErrInvalidCredentials):
    // re-prompt
case errors.Is(err, nwrfc.ErrLogon):
    // generic logon fallback (still NOT a comm error)
case errors.Is(err, nwrfc.ErrCommunication):
    // network / gateway issue
}
```

## Cancellation and mid-call aborts

`Conn.Cancel()` interrupts any in-flight RFC call on the
connection — Open, Ping, Reset, Describe, Invoke. The
backend's `RfcCancel` is the SDK-documented thread-safe
interrupt path (see `docs/EVIDENCE/sdk-cancel.md`).

Cancel is **idempotent** and **goroutine-safe** for the
in-flight cancel use case. A typical pattern:

```go
go func() {
    <-ctx.Done()
    _ = c.Cancel()  // interrupt whatever is blocking
}()
```

Internally the library does this for you on every blocking
op via the shared cancel watcher; you only call `Cancel`
explicitly when something other than ctx makes you want to
abort (a panic-button shutdown signal, etc.).

### SAP caveat for mutating operations

Cancelling a mid-flight Update / Insert / Delete BAPI or any
mutating function module may leave the ABAP side in an
indeterminate state. The ABAP work process may have:

* committed the change before the cancel signal arrived,
* rolled back partially, or
* held an open transaction that another operator must close.

**Library users are responsible** for:

* preferring read-only / idempotent BAPIs on operations they
  intend to be cancellable,
* treating a cancelled mutating call as **outcome unknown**,
* confirming the SAP-side state via a separate read before
  retrying or compensating.

For mutating workloads, prefer:

* generous per-call ctx deadlines instead of mid-call cancel,
* explicit transactional design (tRFC / qRFC / bgRFC),
* SAP-side state confirmation via a separate read after a
  cancellation.

`Conn.Cancel` is safe and recommended for read-only /
idempotent FMs, the handshake during Open, Ping, Describe,
Reset, and any operation classified as `read` or `idempotent`
in the operator's evidence schema (see
[`docs/EVIDENCE/SCHEMA.md`](EVIDENCE/SCHEMA.md)).

## See also

- [PLAN.md §7](PLAN.md#7-error-taxonomy) — the design rationale.
- [SECURITY.md §4](SECURITY.md#4-logging-tracing-and-observability) —
  the redaction policy these types enforce.
- [EVIDENCE/sdk-cancel.md](EVIDENCE/sdk-cancel.md) — RfcCancel
  symbol-presence and thread-safety evidence.
- [ROADMAP_NEXUS_INTEGRATION.md §3](ROADMAP_NEXUS_INTEGRATION.md#3-cancel-implementation-decision-rfccancel-vs-rfccloseconnection)
  — the cancel decision matrix.
