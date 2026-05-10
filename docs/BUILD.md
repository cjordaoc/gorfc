<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Build Notes

Detailed build advice for `gorfc` users. Quick install lives in
[INSTALL.md](INSTALL.md).

## Build constraint cheat-sheet

| Constraint | Files affected |
|---|---|
| `cgo && !nwrfc_nosdk` | `internal/sdkbackend/*.go`, `nwrfc/register_sdk.go` |
| `!cgo || nwrfc_nosdk` | `internal/nosdkbackend/*.go`, `nwrfc/register_nosdk.go` |
| `(linux || darwin || windows) && cgo && !nwrfc_nosdk` | `gorfc/*.go` (legacy upstream package), `example/hello_gorfc.go` |
| (none) | `nwrfc/*.go` (except register_*.go), `internal/backend/*.go`, `internal/ucs2/*.go`, `internal/bcd/*.go`, `internal/timeext/*.go` |

The first three rows are mutually exclusive; exactly one backend
registers per build. The fourth row is pure-Go and compiles in every
configuration.

## Windows/amd64 SDK build on the target host

When the SAP NW RFC SDK for Windows is available only on the Windows VDI,
build `nwrfc.exe` directly on that VDI instead of copying the proprietary SDK
to a Linux build host. The required non-SAP tools are Go for Windows, a
Windows/amd64 C toolchain such as MSYS2 MINGW64 GCC, and the Microsoft Visual
C++ 2015-2022 x64 Redistributable required by current SAP NW RFC SDK DLLs.

```powershell
$env:PATH = "C:\Program Files\Go\bin;C:\msys64\mingw64\bin;$env:PATH"
.\scripts\build-windows-sdk.ps1 `
  -SapNWRFCHome "C:\operator-secure\nwrfcsdk-windows" `
  -Output "C:\nexus-validation\nwrfc.exe" `
  -CC "C:\msys64\mingw64\bin\gcc.exe"
```

The script is idempotent: it validates `include\sapnwrfc.h`,
`lib\sapnwrfc.dll`, and `lib\libsapucum.dll`, sets only process-local cgo
environment variables, prepends the SDK `lib` directory to the build process
`PATH`, builds `.\cmd\nwrfc`, and runs `nwrfc.exe preflight --json` unless
`-SkipPreflight` is supplied. It does not download, copy, commit, or vendor any
SAP SDK file.

The resulting executable is a real cgo build only when it is produced without
`-tags nwrfc_nosdk` and `nwrfc.exe health --json` reports `ok:true` with an SDK
version other than `no-sdk`.

## Cross-compilation

Cgo cross-compilation needs a cross-toolchain. Two reliable patterns:

**Linux → Linux/arm64** with `gcc-aarch64-linux-gnu`:
```bash
sudo apt-get install gcc-aarch64-linux-gnu
export GOOS=linux GOARCH=arm64
export CC=aarch64-linux-gnu-gcc
export CGO_ENABLED=1
export CGO_CFLAGS="-I/path/to/nwrfcsdk-arm64/include"
export CGO_LDFLAGS="-L/path/to/nwrfcsdk-arm64/lib -Wl,-rpath,$ORIGIN"
go build ./...
```

**Linux → Windows/amd64** with `zig cc`:
```bash
export GOOS=windows GOARCH=amd64
export CC="zig cc -target x86_64-windows-gnu"
export CGO_ENABLED=1
export CGO_CFLAGS="-IC:/nwrfcsdk/include"
export CGO_LDFLAGS="-LC:/nwrfcsdk/lib"
go build -o myapp.exe ./...
```

🟡 SAP NWRFC SDK is shipped per-platform; you must have the matching
SDK ZIP for the target OS+arch. Cross-compiling with the wrong SDK
binary always fails at link time.

## CI matrix recommendation

```yaml
# .github/workflows/ci.yml (sketch)
jobs:
  test-nosdk:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
        go: ["1.23", "1.25"]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}
      - run: go test -tags nwrfc_nosdk -race ./...

  test-sdk:
    runs-on: [self-hosted, sap-sandbox]   # CI runner with SDK installed
    env:
      SAPNWRFC_HOME: /opt/sap/nwrfcsdk
      CGO_CFLAGS: "-I/opt/sap/nwrfcsdk/include"
      CGO_LDFLAGS: "-L/opt/sap/nwrfcsdk/lib -Wl,-rpath,/opt/sap/nwrfcsdk/lib"
      GORFC_TEST_USER: ${{ secrets.SAP_USER }}
      GORFC_TEST_PASSWD: ${{ secrets.SAP_PASS }}
      GORFC_TEST_ASHOST: ${{ secrets.SAP_ASHOST }}
      GORFC_TEST_SYSNR: "00"
      GORFC_TEST_CLIENT: "100"
    steps:
      - uses: actions/checkout@v4
      - run: go test -race ./...
```

The `test-nosdk` job runs on every PR; `test-sdk` runs nightly because
SAP sandbox time is rate-limited.

## Linker hardening

Production builds should:

1. Ship `libsapnwrfc.{so,dll,dylib}` and `libsapucum.{so,dll,dylib}`
   alongside the binary, never inside the binary.
2. Use `rpath=$ORIGIN/lib` (Linux) so the binary finds the SAP libs
   relative to itself; avoids global `LD_LIBRARY_PATH` poisoning.
3. Strip debug info (`go build -ldflags '-s -w'`) only after
   verifying no SAP-related debug symbols are needed for crash
   triage. The cgo bridge calls into SAP code; `addr2line` against
   stripped binaries will not resolve SAP-side frames in any case.

## Reproducible builds

Set `GOFLAGS=-trimpath -buildvcs=false` and pin the SAP SDK release
(record the PL number in the build manifest). Two build runs with the
same `go.sum`, `SAPNWRFC_HOME` SDK PL, and `CC` produce
byte-identical binaries on Linux+amd64.

## IDE / gopls configuration

The cgo files under `internal/sdkbackend/*.go` and the legacy
`gorfc/gorfc.go` carry `//go:build cgo && !nwrfc_nosdk`. They
`#include <sapnwrfc.h>`, so on a developer machine without the
SAP NW RFC SDK headers installed locally, gopls will report
`undefined: C.RfcOpenConnection` and similar errors when it
tries to index those files.

This is **not** a build problem (the no-SDK build excludes the
files entirely; the SDK build needs the headers anyway). It is
a per-developer LSP indexing concern. Two ways to silence it:

**Option A** — point gopls at the SDK headers locally
(recommended when you do have the SDK installed):

```jsonc
// .vscode/settings.json (per developer; .vscode is gitignored)
{
  "go.toolsEnvVars": {
    "CGO_CFLAGS":  "-I/opt/sap/nwrfcsdk/include",
    "CGO_LDFLAGS": "-L/opt/sap/nwrfcsdk/lib"
  }
}
```

**Option B** — tell gopls to index the no-SDK side only, so
the cgo files are simply excluded from indexing:

```jsonc
// .vscode/settings.json (per developer; .vscode is gitignored)
{
  "gopls": {
    "build.buildFlags": ["-tags=nwrfc_nosdk"]
  }
}
```

Both options are local to the developer machine. The repo
deliberately does not commit `.vscode/` (see `.gitignore`).

For Vim / Neovim with `coc-go` or `gopls` direct, set
`build.buildFlags` to `["-tags=nwrfc_nosdk"]` in the LSP
settings.

## See also

- [INSTALL.md](INSTALL.md) — quickstart per OS.
- [DEPLOY.md](DEPLOY.md) — VDI / production deployment.
- [SDK_FUNCTIONS_MAP.md](SDK_FUNCTIONS_MAP.md) — every SDK function
  the binding uses, with verification status.
- [PLAN.md §6](PLAN.md#6-internal-cgo-binding-strategy) — the cgo
  binding strategy in depth.
