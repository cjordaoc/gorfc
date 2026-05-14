<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Build Notes

Detailed build advice for `gorfc` users. Quick install lives in
[INSTALL.md](INSTALL.md).

## Build constraint cheat-sheet

| Constraint | Files affected |
|---|---|
| `cgo && !nwrfc_nosdk` | `internal/sdkbackend/*.go`, `nwrfc/register_sdk.go` |
| `gorfc_sdktest && cgo && !nwrfc_nosdk` | `internal/sdktest/*.go` (SDK probe package, opt-in) |
| `!cgo || nwrfc_nosdk` | `internal/nosdkbackend/*.go`, `nwrfc/register_nosdk.go` |
| `(linux || darwin || windows) && cgo && !nwrfc_nosdk` | `gorfc/*.go` (legacy upstream package), `example/hello_gorfc.go` |
| (none) | `nwrfc/*.go` (except register_*.go), `internal/backend/*.go`, `internal/ucs2/*.go`, `internal/bcd/*.go`, `internal/timeext/*.go` |

The `cgo && !nwrfc_nosdk` and `!cgo || nwrfc_nosdk` rows are mutually
exclusive; exactly one backend registers per build. The last row is
pure-Go and compiles in every configuration. The
`gorfc_sdktest` row is an opt-in probe/validation package — it is not
part of the production binding and only compiles when the
`gorfc_sdktest` build tag is passed explicitly (see the AddressSanitizer
SDK lane below).

## IDE and gopls workspace (SDK-free mode)

**The problem.** `gopls` (and `go list` / `go build` underneath it)
compiles the cgo packages by default. When a contributor opens this
workspace in VS Code or any gopls-based IDE without the SAP NWRFC SDK
installed and `CGO_CFLAGS`/`CGO_LDFLAGS` configured, the LSP tries to
build `internal/sdkbackend/*.go` and `gorfc/*.go` and produces hundreds
of cascade errors rooted in `sapnwrfc.h: No such file or directory`.

**The solution.** Tell gopls to build with `-tags nwrfc_nosdk`. That
selects the no-SDK stub backend, which is pure-Go and compiles with no
SDK headers or libraries present. This is an IDE/editor setting only —
it does not change the default build model for `go build` or CI.

**VS Code** — commit or create `.vscode/settings.json`:

```json
{
  "go.buildFlags": ["-tags", "nwrfc_nosdk"],
  "gopls": {
    "build.buildFlags": ["-tags", "nwrfc_nosdk"]
  }
}
```

**GoLand / IntelliJ** — Settings → Go → Build Tags & Vendoring →
**Custom tags**: `nwrfc_nosdk`.

**CLI / other editors** — export the flag for the gopls process, or
pass it directly to go commands:

```bash
GOFLAGS="-tags=nwrfc_nosdk" gopls
go test -tags nwrfc_nosdk ./...
```

**What this mode does.** It links the no-SDK stub backend
(`internal/nosdkbackend`). Every RFC operation returns a
`*nwrfc.SDKUnavailableError` at runtime. It lets the workspace compile,
`go test ./...` pass, and gopls resolve types and references without the
SDK.

**What this mode does NOT do.** It does not validate SDK behavior, cgo
memory handling, or live SAP calls. The cgo backend in
`internal/sdkbackend` is never compiled under this tag, so nothing in
that path — including header bindings and `RfcOpen*`/`Rfc*` wrappers —
is type-checked or exercised. SDK-backed verification still requires the
SDK-backed build.

**Switching back to SDK-backed mode.** Remove the `nwrfc_nosdk` build
flag from your IDE/gopls settings (and unset `GOFLAGS` if you set it
there), then configure `CGO_CFLAGS` and `CGO_LDFLAGS` to point at the
SAP NWRFC SDK as described in [INSTALL.md](INSTALL.md). The default
build (no tag) selects the cgo SDK backend.

**Caveat — `internal/sdktest` and `gorfc/`.** The `nwrfc_nosdk` tag does
**not** make the whole repository SDK-free. `internal/sdktest` is an
opt-in SDK probe package constrained to
`gorfc_sdktest && cgo && !nwrfc_nosdk`, and the legacy upstream `gorfc/`
package is constrained to
`(linux || darwin || windows) && cgo && !nwrfc_nosdk` — both still
require the SDK to compile when their tags are active, regardless of this
flag. They are separate concerns: scope your IDE to the modern `nwrfc/`
packages, or expect those two to stay un-buildable until the SDK is
configured.

## Connection-open timeout

`context.WithTimeout` around `nwrfc.Open` is checked before gorfc calls
the backend. In the cgo backend, once `RfcOpenConnection` has been
called, gorfc has no `RFC_CONNECTION_HANDLE` yet and cannot use
`RfcCancel` to interrupt the open. The process returns only when the SDK
returns, so build and runtime environments must rely on SAP NWRFC SDK,
SAP gateway/SAProuter, and operating-system network timeout settings for
in-flight connection attempts.

T3 validation record (2026-05-14): the local workspace does not contain
`sapnwrfc.h`, `libsapnwrfc`, `sapnwrfc.dll`, a SDK ZIP, or a configured
test destination, and the documented example dispatcher endpoint was not
reachable from this host. No SDK-level connection parameter has been
verified by this repository as bounding `RfcOpenConnection`. The
documented SAP-owned CPIC setup bound is the gateway profile parameter
`gw/cpic_timeout`; application owners that need a hard wall-clock bound
must enforce it outside the process with a supervisor, batch runner, or
orchestrator that can terminate a process blocked in the native SDK.

For portable application builds:

1. Prefer destination configuration (`sapnwrfc.ini`, `RFC_INI`, or
   `nwrfc.SetIniPath`) for SAP-owned timeout parameters such as
   `gw/cpic_timeout`.
2. Pass SDK parameters not modeled by `nwrfc.Params` through
   `Params.Extra`; gorfc forwards them to `RfcOpenConnection`, but this
   repository does not currently verify any SDK-level open-timeout key.
3. Keep service supervisors, Kubernetes probes, and batch job timeouts
   outside the process as the last line of defense for a native SDK call
   blocked below Go.

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

## Code generation workflow

`cmd/nwrfc-gen` separates live SAP metadata capture from offline code
generation. Commit the JSON descriptor, not SAP credentials or
environment-specific connection details.

Capture a descriptor from a SAP test system:

```bash
nwrfc-gen describe --fn STFC_CONNECTION --out descriptors/stfc_connection.json
```

Generate a typed client package from the committed descriptor:

```bash
nwrfc-gen generate \
  --json descriptors/stfc_connection.json \
  --pkg stfc \
  --out internal/stfc/
```

The generated package contains `In`, `Out`, `InOut`, `Call`, and
`CallWithPool`. It also writes an SDK-free `_test.go` that uses
`nwrfcmock`, so downstream CI can run:

```bash
go test -tags nwrfc_nosdk ./internal/stfc
```

For `go generate`, keep the command next to the generated package:

```go
//go:generate nwrfc-gen generate --json ../../descriptors/stfc_connection.json --pkg stfc --out .
```

Live generation is available for local development:

```bash
nwrfc-gen generate --fn STFC_CONNECTION --save-json descriptors/stfc_connection.json --pkg stfc --out internal/stfc/
```

The live modes read the same `GORFC_TEST_*` variables used by integration
tests (`GORFC_TEST_DEST` or `GORFC_TEST_ASHOST`/`GORFC_TEST_SYSNR`, plus
client/user/auth fields). Do not commit those values.

## AddressSanitizer SDK lane

`.github/workflows/asan.yml` defines an opt-in AddressSanitizer lane for
the cgo backend, SDK-only tests, and one protected live SAP RFC call path.
It is disabled for ordinary public PRs because it requires SAP NWRFC SDK
libraries and SAP test credentials that must never be committed or
exposed to forked builds.

This lane deliberately does **not** run the legacy live SAP integration
tests in `gorfc/gorfc_test.go`. Those tests include
`ConnectionFromDest("MME")` / `ConnectionFromDest("QM7")` cases that
require a resolvable `RFC_INI` destination file and broader function
coverage. The ASan contract is narrower and explicit:

1. Load the SAP NWRFC SDK.
2. Run SDK-local cgo helper tests that do not require a SAP system.
3. Run `TestASAN_STFCStructure_MarshalingRoundTrip` under `go test -asan`.

The live ASan test opens a protected SAP test connection and invokes
`STFC_STRUCTURE` through `nwrfc.CallMap`. Its payload includes string,
byte, structure, and table parameters, so the run traverses both
`internal/sdkbackend/fill.go` and `internal/sdkbackend/wrap.go` under
AddressSanitizer. A green ASan workflow is still not a full SAP RFC
conformance result; it is targeted leak validation for this SDK-backed
marshaling/unmarshaling path.

Enable it in GitHub Actions by provisioning a protected self-hosted Linux
runner with the labels `self-hosted`, `linux`, and `sap-sandbox`, then set
repository variable `GORFC_TEST_ASAN=1` or run the workflow manually with
the `asan` input checked. The workflow expects these repository variables:

| Variable | Purpose |
|---|---|
| `SAPNWRFC_HOME` | SDK install root on the runner |
| `SAPNWRFC_CFLAGS` | include flags, for example `-I/opt/sap/nwrfcsdk/include` |
| `SAPNWRFC_LDFLAGS` | library flags, for example `-L/opt/sap/nwrfcsdk/lib -Wl,-rpath,/opt/sap/nwrfcsdk/lib` |
| `SAPNWRFC_LIBRARY_PATH` | runtime loader path for `LD_LIBRARY_PATH` |

It expects these protected secrets:

| Secret | Purpose |
|---|---|
| `GORFC_TEST_USER` | SAP user with `S_RFC` authorization for `STFC_STRUCTURE` |
| `GORFC_TEST_PASSWD` | password for the protected SAP test user |
| `GORFC_TEST_ASHOST` | SAP application server host |
| `GORFC_TEST_SYSNR` | instance number |
| `GORFC_TEST_CLIENT` | client / mandant |
| `GORFC_TEST_LANG` | optional logon language, for example `EN` |

The ASan integration test also supports `GORFC_TEST_DEST` for local
runs that use destination lookup, but the GitHub workflow is wired to
direct connection secrets so it does not depend on a committed or
runner-local `sapnwrfc.ini`. Do not print these values in workflow logs.

Local Linux run:

```bash
export GORFC_TEST_ASAN=1
export SAPNWRFC_HOME=/opt/sap/nwrfcsdk
export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,$SAPNWRFC_HOME/lib"
export LD_LIBRARY_PATH="$SAPNWRFC_HOME/lib"
export ASAN_OPTIONS="detect_leaks=1:halt_on_error=1:abort_on_error=1"

export GORFC_TEST_USER="$SAP_USER"
export GORFC_TEST_PASSWD="$SAP_PASSWORD"
export GORFC_TEST_ASHOST="$SAP_ASHOST"
export GORFC_TEST_SYSNR="00"
export GORFC_TEST_CLIENT="100"
export GORFC_TEST_LANG="EN"

test "$GORFC_TEST_ASAN" = "1"
go test -asan -tags gorfc_sdktest ./internal/sdktest
go test -asan ./nwrfc -run '^(TestSDK|TestCapabilities|TestLanguage|TestSetTrace|TestNewTID|TestNewUnitID|TestUnitState|TestNewQueuedTransaction)'
go test -asan ./nwrfc -count=1 -run '^TestASAN_STFCStructure_MarshalingRoundTrip$'
```

`internal/sdktest` is an opt-in SDK probe package: its files carry the
`gorfc_sdktest && cgo && !nwrfc_nosdk` build constraint, so it does not
compile in the default workspace (even with cgo active) and will not
cause cascade errors from `sapnwrfc.h` in an IDE that has cgo on but no
SDK configured. You must pass `-tags gorfc_sdktest` to build or test it.

The ASan lane is meant to catch C-memory mistakes in paths that allocate
through SDK/cgo helpers, including SAP_UC encode/decode probes in
`internal/sdktest` and SDK-backed public wrappers in `nwrfc`. The live
ASan call path uses the same direct connection variables as the broader
integration-test contract, but it runs only the single `STFC_STRUCTURE`
marshaling round-trip needed to cover the targeted `fill.go` and
`wrap.go` allocation paths.

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

## See also

- [INSTALL.md](INSTALL.md) — quickstart per OS.
- [SDK_FUNCTIONS_MAP.md](SDK_FUNCTIONS_MAP.md) — every SDK function
  the binding uses, with verification status.
- [PLAN.md §6](PLAN.md#6-internal-cgo-binding-strategy) — the cgo
  binding strategy in depth.
