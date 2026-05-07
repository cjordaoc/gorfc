<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Installation

> Status: Tier 1 (v0.1.0). API surface is stable; ABI is not.

## What you need

| Component | Minimum | Notes |
|---|---|---|
| Go | 1.23 with `toolchain go1.25.0` | `go.mod` directive; the toolchain auto-fetches 1.25 if not present. |
| SAP NetWeaver RFC SDK | 7.50 PL12 (PL18 recommended) | Download from the [SAP Software Center](https://support.sap.com/en/product/connectors/nwrfcsdk.html) under your own SAP entitlement. |
| C toolchain | gcc on Linux, MinGW-w64 or `zig cc` on Windows, Xcode CLI tools on macOS | cgo links to libsapnwrfc / libsapucum at build time. |
| (Optional) SAP CommonCryptoLib | Latest from SAP Support | Required only if you need SNC or WebSocket-RFC TLS. |

This project does NOT redistribute the SDK or CommonCryptoLib. See
[SECURITY.md](SECURITY.md) §7 for the license boundary.

## Linux (recommended path)

```bash
# 1. Extract the SDK (downloaded as nwrfc750P_*-* SAR)
sudo mkdir -p /opt/sap
sudo tar -xf nwrfc750P_*-*.tar -C /opt/sap        # or unar / unrar
ls /opt/sap/nwrfcsdk/lib                          # should show libsapnwrfc.so

# 2. Set the environment
export SAPNWRFC_HOME=/opt/sap/nwrfcsdk
export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,$SAPNWRFC_HOME/lib"

# 3. Build / test
go build ./...
go test  ./...
```

The `-Wl,-rpath` lets the resulting binary find `libsapnwrfc.so` at
runtime without `LD_LIBRARY_PATH`. If you ship the binary, package
the SDK separately and set `LD_LIBRARY_PATH=$SAPNWRFC_HOME/lib` in the
runtime unit / container init script.

## Windows (x86_64, MinGW-w64)

```cmd
:: 1. Extract the SDK
:: Assume C:\nwrfcsdk

:: 2. Environment
set SAPNWRFC_HOME=C:\nwrfcsdk
set CGO_CFLAGS=-IC:\nwrfcsdk\include
set CGO_LDFLAGS=-LC:\nwrfcsdk\lib

:: 3. Build with MinGW-w64 in PATH (or use `zig cc` as the
:: C compiler via CC=zig\zig.exe cc)
go build ./...
```

Windows `libsapnwrfc.dll` and `libsapucum.dll` must be on `PATH` at
runtime. Common pattern: copy them to the same directory as the
binary, or extend `PATH` in the service start unit.

## macOS (best-effort)

```bash
export SAPNWRFC_HOME=/usr/local/sap/nwrfcsdk
export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,$SAPNWRFC_HOME/lib"
export DYLD_LIBRARY_PATH=$SAPNWRFC_HOME/lib    # macOS Gatekeeper requires this

go build ./...
```

macOS support is tier-2 (best-effort). The SAP NWRFC SDK ships with
macOS dylibs but the SAP unicode header `sapuc.h` includes a `wchar.h`
shim that occasionally interacts poorly with Apple-clang's
`__has_include`. If you hit a header issue, use Linux through Docker
or Lima as the fallback build environment.

## Build modes

| Mode | Tag | What you get |
|---|---|---|
| Default | (none) | cgo backend; full RFC functionality. Requires SDK headers + libs. |
| SDK-free | `-tags nwrfc_nosdk` | No-SDK stub backend. Every operation returns `*nwrfc.SDKUnavailableError`. Useful for downstream packages that re-export `nwrfc` types but don't connect to SAP. |
| No cgo | `CGO_ENABLED=0` | Same as `-tags nwrfc_nosdk`; the no-SDK stub is the only valid choice when cgo is off. |

```bash
# Build a binary that compiles in any CI but errors at runtime
# if anyone tries to actually connect.
CGO_ENABLED=0 go build -o myapp ./cmd/myapp
```

## Verification

After building, run `nwrfc.EnsureSDK()` at process start to fail-fast
on misconfigured deployments:

```go
package main

import (
    "log"
    "github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
    if err := nwrfc.EnsureSDK(); err != nil {
        log.Fatalf("nwrfc: %v", err)
    }
    log.Printf("nwrfc: SDK %s loaded; capabilities=%+v",
        nwrfc.SDKVersion(), nwrfc.Capabilities())
    // ... start the rest of the service
}
```

For a sanity check against a real SAP system, see the
[examples/](../example/) directory and the GORFC_TEST_* environment
variables in [SECURITY.md](SECURITY.md) §3.

## See also

- [BUILD.md](BUILD.md) — deeper notes on cross-compilation, sentinels,
  and CI matrices.
- [CONFIGURATION.md](CONFIGURATION.md) — runtime configuration
  (Params, IniFS, providers).
- [ERRORS.md](ERRORS.md) — typed error hierarchy and retry semantics.
- [SECURITY.md](SECURITY.md) — credential rules and redaction.
