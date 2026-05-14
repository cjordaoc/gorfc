<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Runtime Configuration

How to populate `nwrfc.Params`, where the SDK looks for `sapnwrfc.ini`,
and how to thread per-call options through.

## nwrfc.Params

```go
type Params struct {
    // Destination: looked up via sapnwrfc.ini
    Dest string

    // Direct connection
    AsHost, SysNr string

    // Load-balanced via Message Server
    MsHost, R3Name, Group string

    // WebSocket RFC
    WSHost, WSPort string

    // Logon
    Client, User, Passwd, Lang string

    // Auth alternatives (mutually exclusive with Passwd)
    Mysapsso2, X509Cert, SAML2, Bearer string

    // SNC
    SncQOP, SncLib, SncMyName, SncPartnerName, SncSso string

    // TLS for WebSocket RFC
    TLSClientPSE, TLSTrustAll string

    // Operational
    Trace string

    // Escape hatch for SDK params not modeled
    Extra map[string]string
}
```

`Params.LogValue` redacts sensitive fields automatically per
`backend.SensitiveKeys`. You can pass a `Params` to `slog.Info` or
embed it in a span attribute without leaking credentials.

## Validation

`nwrfc.Open(ctx, p)` checks `ctx.Err()` first. If the context is still
active, it calls `p.validate()` before reaching the SDK and fails fast
with `*nwrfc.ConfigError` on:

- No transport set (none of Dest, AsHost+SysNr, MsHost+R3Name+Group,
  WSHost+WSPort).
- AsHost without SysNr.
- MsHost without R3Name+Group.
- WSHost without WSPort.
- More than one auth method (Passwd + Mysapsso2, etc.).

```go
_, err := nwrfc.Open(ctx, nwrfc.Params{User: "u"})
if errors.Is(err, nwrfc.ErrConfig) {
    var ce *nwrfc.ConfigError
    errors.As(err, &ce)
    log.Printf("config error in %s: %s", ce.Field, ce.Hint)
}
```

## Open cancellation and connection timeout

`nwrfc.Open(ctx, p)` checks `ctx.Err()` before validation and before
dispatching to the active backend. If the context is already cancelled or
expired, `Open` returns that context error and does not call the SDK.

Once the cgo backend has entered `RfcOpenConnection`, gorfc cannot cancel
the operation with `RfcCancel`: the SAP NWRFC SDK only returns an
`RFC_CONNECTION_HANDLE` after a successful open, and `RfcCancel` needs an
existing handle. A `context.WithTimeout` deadline around `Open` is
therefore a pre-call guard, not a guaranteed interrupt for an in-flight
SDK connection attempt.

Control connection-open timeout through the SAP/SAProuter/gateway and
network configuration for the destination. SAP documents
[`RfcOpenConnection`](https://help.sap.com/doc/saphelp_nw75/7.5.5/en-US/48/b0ff6b792d356be10000000a421937/content.htm)
as the client-open entrypoint and lists the accepted connection parameter
families (`ashost`/`sysnr`, `mshost`/message-server fields, SNC, SSO
ticket, or `dest`). SAP also documents that
[`sapnwrfc.ini`](https://help.sap.com/saphelp_em92/helpdata/de/48/ce50e418d3424be10000000a421937/content.htm)
can carry RFC-specific parameters without code changes. Gateway-level
connection timeout behavior is controlled by SAP profile parameters such
as
[`gw/cpic_timeout`](https://help.sap.com/saphelp_gbt10/helpdata/DE/48/ad5afa8bc96744e10000000a421937/content.htm)
for CPIC connection setup.

T3 validation record (2026-05-14): this checkout has no configured SAP
NWRFC SDK or test destination, so the SAP-backed command could not enter
`RfcOpenConnection`. The local probe also could not reach the documented
example dispatcher endpoint. Do not document or depend on an unnamed
SDK-level open-timeout parameter in `Params.Extra` or `sapnwrfc.ini` for
this release. The concrete SAP-owned limit documented for CPIC connection
setup is the gateway profile parameter `gw/cpic_timeout`; when an operator
needs a shorter bound than the SAP landscape provides, the documented
application workaround is an external process supervisor, job runner, or
orchestrator timeout that can terminate the process blocked inside the
native SDK call. gorfc deliberately does not spawn an abandoned
`RfcOpenConnection` goroutine as a synthetic fallback.

## sapnwrfc.ini

The SDK reads `sapnwrfc.ini` from (in order):

1. The directory the executable runs in.
2. The directory `RfcSetIniPath` last set (via `nwrfc.SetIniPath(dir)`).
3. The directory `RFC_INI` env var points to.
4. The current working directory.

To use a destination by name:

```go
conn, err := nwrfc.OpenDest(ctx, "DEV")  // looks up [DEST=DEV] block
```

A placeholder profile lives at
[gorfc/sapnwrfc.ini.example](../gorfc/sapnwrfc.ini.example). Copy it
to `sapnwrfc.ini` and edit, but do NOT commit your edited copy —
`.gitignore` blocks `**/sapnwrfc.ini` for that reason
([SECURITY.md §2](SECURITY.md#2-never-commit)).

## CallOptions

Per-call overrides:

```go
nwrfc.Call(ctx, conn, "BAPI_USER_GET_DETAIL", in, &out, nwrfc.CallOptions{
    NotRequested:       []string{"PARAMETER1", "PARAMETER2"}, // skip on SAP side
    ReturnImportParams: true,                                  // PyRFC compat
    AllowZeroDate:      true,                                  // accept 00000000
    CheckTime:          true,                                  // strict TIMS parser
})
```

Default behavior is strict on date/time, lenient on RStrip (CHAR
trailing space removed by default).

## Pool

```go
p, _ := nwrfc.NewPool(nwrfc.PoolConfig{
    Params:         nwrfc.Params{ /* ... */ },
    MinSize:        2,
    MaxSize:        16,
    IdleTimeout:    5 * time.Minute,
    MaxLifetime:    30 * time.Minute,
    AcquireTimeout: 5 * time.Second,
    AfterAcquire: func(ctx context.Context, c *nwrfc.Conn) error {
        return c.Reset()  // wipe ABAP context between checkouts
    },
})
defer p.Close()

err := p.Do(ctx, func(c *nwrfc.Conn) error {
    _, err := nwrfc.Call(ctx, c, "RFC_PING", nil, nil)
    return err
})
```

## Trace control

```go
nwrfc.SetTraceLevel(0)            // off (default; required for production)
nwrfc.SetTraceLevel(1)            // logon trace only
nwrfc.SetTraceLevel(2)            // payload trace — DOES capture business data
nwrfc.SetTraceDir("/var/log/sap") // redirect trace files
```

See [SECURITY.md §5](SECURITY.md#5-trace-control) for the security
implications of trace level >= 2.

## Capability detection

```go
caps := nwrfc.Capabilities()
if !caps.WebSocketRFC {
    log.Print("SDK is too old for WebSocket RFC; falling back to CPIC")
}
```

A request that requires a missing capability returns
`*nwrfc.UnsupportedFeatureError` carrying the required + current SDK
versions.

## See also

- [INSTALL.md](INSTALL.md) — installing the SDK.
- [ERRORS.md](ERRORS.md) — error semantics and retry guidance.
- [SECURITY.md](SECURITY.md) — never log Params via `%v`; always via
  `slog.LogValuer`.
