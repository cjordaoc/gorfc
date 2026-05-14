<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Migrating from upstream `github.com/sap/gorfc`

The community revival has the same purpose as the archived
upstream — a Go binding over the SAP NetWeaver RFC SDK — but
the public API is rewritten under the new module path
`github.com/cjordaoc/gorfc/nwrfc`. This document is the
mechanical port guide.

## TL;DR

- Change the import: `github.com/sap/gorfc/gorfc` →
  `github.com/cjordaoc/gorfc/nwrfc`.
- Replace `Connection` / `ConnectionFromParams` /
  `ConnectionFromDest` with `Conn` / `Open` / `OpenDest`.
- Pass a `context.Context` as the first argument everywhere.
- Replace `*RfcError` / `*GoRfcError` checks with the typed
  hierarchy in `nwrfc/errors.go`.
- Replace `map[string]any` payloads with typed structs and
  `rfc:"..."` struct tags (or keep maps via `nwrfc.CallMap`).
- Acknowledge that ABAP "00000000"/"000000" no longer
  silently maps to a zero `time.Time` — opt back in with
  `nwrfc.CallOptions{AllowZeroDate: true}` per call.

## Side-by-side

### Open a connection

```go
// Before (upstream)
import "github.com/sap/gorfc/gorfc"

c, err := gorfc.ConnectionFromParams(gorfc.ConnectionParameters{
    "user":   "X", "passwd": "Y",
    "ashost": "sap.example.invalid", "sysnr": "00",
    "client": "100", "lang": "EN",
})
if err != nil { return err }
defer c.Close()
```

```go
// After (revival)
import (
    "context"
    "time"
    "github.com/cjordaoc/gorfc/nwrfc"
)

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

c, err := nwrfc.Open(ctx, nwrfc.Params{
    User: "X", Passwd: "Y",
    AsHost: "sap.example.invalid", SysNr: "00",
    Client: "100", Lang: "EN",
})
if err != nil { return err }
defer c.Close()
```

### Call an RFC

```go
// Before
result, err := c.Call("STFC_CONNECTION", map[string]interface{}{
    "REQUTEXT": "ping",
})
echo := result["ECHOTEXT"].(string)
```

```go
// After (typed)
type In  struct { ReqText string `rfc:"REQUTEXT"` }
type Out struct {
    EchoText string `rfc:"ECHOTEXT"`
    RespText string `rfc:"RESPTEXT"`
}
var out Out
_, err := nwrfc.Call(ctx, c, "STFC_CONNECTION", In{ReqText: "ping"}, &out)
echo := out.EchoText
```

```go
// After (dynamic)
resp, err := nwrfc.CallMap(ctx, c, "STFC_CONNECTION", backend.CallParams{
    "REQUTEXT": "ping",
})
echo := resp["ECHOTEXT"].(string)
```

### Error handling

```go
// Before
if rfcErr, ok := err.(*gorfc.RfcError); ok {
    fmt.Println(rfcErr.ErrorInfo.Code)
}
```

```go
// After
import "errors"

if errors.Is(err, nwrfc.ErrLogon) { /* re-prompt */ }

var abap *nwrfc.ABAPApplicationError
if errors.As(err, &abap) {
    fmt.Println(abap.AbapMsgClass, abap.AbapMsgNumber)
}

if nwrfc.IsRetryable(err) {
    // try again with a fresh connection
}
```

## Behavioral changes you must know

### Date / Time policy

Upstream silently translated "00000000" (ABAP initial date) to
the zero `time.Time`. The revival fails with `*MarshalError`
wrapping `nwrfc.ErrZeroDate` — explicit per AGENTS.md "no
silent fallback".

To preserve the upstream behavior:

```go
nwrfc.Call(ctx, c, fn, in, &out, nwrfc.CallOptions{
    AllowZeroDate: true,
    AllowZeroTime: true,
})
```

### Memory leak fix

Upstream `fillVariable` had a `defer C.free` that captured a
nil pointer at defer-time, leaking SAP_UC allocations on every
fill. The legacy package gets the fix in T0.3; the revival
sdkbackend uses paired `mallocU` / `freeU` helpers throughout.

Migration impact: workloads that previously suffered slow
memory growth on long-running connections should see flat
memory after migrating.

### Build tag

Upstream's build tag `(linux && cgo) || (amd64 && cgo) ||
(darwin && cgo)` matched any amd64 GOOS — including FreeBSD,
OpenBSD, etc. — and produced confusing cgo errors. The new
constraint is `(linux || darwin || windows) && cgo`. If you
were silently building on an unsupported GOOS, it now fails
fast with `build constraints exclude all Go files`.

### Hardcoded SDK paths removed

Upstream's `gorfc/gorfc.go` carried hardcoded `#cgo` directives
with absolute SDK paths (`-IC:/Tools/nwrfcsdk/include/`,
`-I/usr/local/sap/nwrfcsdk/include`, matching `-L...` LDFLAGS,
and a darwin `-Wl,-rpath,...`). Those have been removed.

The legacy `gorfc/` package no longer hardcodes any SDK path and
now requires the **same** `CGO_CFLAGS` / `CGO_LDFLAGS` setup as
the modern `internal/sdkbackend` backend — only the
path-independent compile-time defines and the `-lsapnwrfc` /
`-lsapucum` library names remain in the source. Set, for
example:

```sh
export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,$SAPNWRFC_HOME/lib"
```

See [docs/INSTALL.md](INSTALL.md) for the portable build pattern.
Builds without these env vars set fail explicitly at the cgo
header step (`fatal error: sapnwrfc.h: No such file or
directory`).

### `nwrfc_nosdk` build tag

The revival adds a no-SDK build mode (`-tags nwrfc_nosdk`) that
swaps in a stub backend returning `*SDKUnavailableError` on
every operation. Useful for downstream libraries that re-export
gorfc types but never call into SAP, and for SDK-free CI.
There is no upstream equivalent.

### Logging

Upstream did not redact secrets. The revival's `slog.LogValuer`
implementations on Params and every error type strip
PASSWD/MYSAPSSO2/X509CERT/SNC partner names automatically.
Combined with `nwrfcotel.RedactHandler`, even raw maps cannot
leak secrets.

If you previously wrote `log.Printf("connect %v", params)` and
were lucky the password was not on stdout, you no longer need
to be lucky.

## Module path

If you cannot change the import path immediately, the legacy
package is preserved at `gorfc/` for one minor release. It
still uses the old module-internal import as a relative
alias when built with `cgo && !nwrfc_nosdk`. New work should
use `nwrfc/` exclusively.

## Pool, Session, Server, Codegen

These features did not exist in upstream. See the corresponding
docs:

- [docs/CONFIGURATION.md](CONFIGURATION.md#pool) for `nwrfc.Pool`
- [docs/PLAN.md §5.7](PLAN.md#5-public-api-proposal) for
  `nwrfc.Session`
- [docs/PLAN.md §5.14](PLAN.md#5-public-api-proposal) for
  `nwrfc.Server` (Tier 2.7)
- `cmd/nwrfc-gen` for typed BAPI client codegen (Tier 4.2)

## Tested with

- Linux x86_64 + Go 1.26 + SAP NetWeaver RFC SDK 7.50 PL18.
- 🟡 Windows x86_64 + MinGW-w64: pending CI runner.
- 🟡 macOS arm64: pending tier-2 best-effort verification.
