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

`nwrfc.Open(ctx, p)` calls `p.validate()` before reaching the SDK and
fails fast with `*nwrfc.ConfigError` on:

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
