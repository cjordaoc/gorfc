<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Deployment Playbook — VDI / Servers

> Audience: SREs and operators packaging gorfc-based binaries
> for Linux (RHEL / Ubuntu LTS), Windows Server 2019+, Windows
> 10/11 Enterprise, Citrix, VMware Horizon, AWS WorkSpaces, and
> Azure Virtual Desktop.

This document is the runtime / packaging companion to
[INSTALL.md](INSTALL.md) (build) and
[BUILD.md](BUILD.md) (cross-compilation). It assumes the
binary already builds; the focus here is shipping it onto
locked-down corporate environments without admin rights or
control over `PATH` / `LD_LIBRARY_PATH`.

The non-negotiables come from
[AGENTS.md](../AGENTS.md): no SDK / credential redistribution,
no silent fallback, fail-fast at process start.

---

## 1. Why VDI is special

VDI environments share a few hostile traits:

* **PATH and LD_LIBRARY_PATH are user-controlled and rewritten**
  by login scripts, antivirus shims, and Citrix profile redirection.
  Relying on them is unreliable and unauditable.
* **No admin rights**, so no system-wide installs to
  `Program Files` / `/usr/local`, no registry edits.
* **App-V / Citrix profile copy-on-write** can move the binary
  away from its sibling files between sessions.
* **Antivirus** may quarantine cgo-loaded native DLLs the
  first time they appear; corporate AV exemptions live in
  per-binary allowlists — not in `PATH` heuristics.
* **The user is non-technical** and cannot be asked to set
  environment variables.

The packaging strategy below sidesteps all of these by
co-locating the runtime SAP NW RFC SDK shared libraries with
the executable and building with relocatable rpath / PE-loader
defaults.

## 2. Linux VDI (RHEL / Ubuntu LTS)

### 2.1 Build with `$ORIGIN`-relative rpath

```bash
export SAPNWRFC_HOME=/opt/sap/nwrfcsdk           # build host only
export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,'$ORIGIN/lib'"
go build -trimpath -ldflags "-s -w" -o bin/app ./cmd/myapp
```

The crucial flag is `-Wl,-rpath,'$ORIGIN/lib'`. At runtime the
ELF dynamic loader (`ld.so`) looks for `libsapnwrfc.so` in
`$ORIGIN/lib` first, where `$ORIGIN` is the directory of the
running binary. No `LD_LIBRARY_PATH` is needed.

> Note the **single quotes** around `$ORIGIN`. Without them,
> the shell expands `$ORIGIN` to empty before `gcc` ever sees
> it, and the rpath ends up empty.

### 2.2 Layout to ship

```
deploy/
├── bin/
│   └── app                   ← Go binary built per §2.1
└── lib/
    ├── libsapnwrfc.so        ← from SAP NW RFC SDK 7.50 PL ≥ floor
    └── libsapucum.so
```

If the deployment also needs SAP CommonCryptoLib (SNC,
WebSocket TLS), drop `libsapcrypto.so` into `lib/` alongside
the others. Then call `nwrfc.LoadCryptoLibrary("lib/libsapcrypto.so")`
during process start.

### 2.3 Verify at runtime

```bash
./bin/app -ensure-sdk    # if your binary exposes a flag for this
```

Or programmatically at process start:

```go
if err := nwrfc.EnsureSDK(); err != nil {
    log.Fatalf("nwrfc: %v", err)
}
log.Printf("nwrfc: SDK %s loaded; capabilities=%+v",
    nwrfc.SDKVersion(), nwrfc.Capabilities())
```

### 2.4 Anti-patterns to avoid

| Don't | Why |
|---|---|
| `LD_LIBRARY_PATH=/some/path` in the user profile | Citrix / GPO routinely overrides this; AV may strip it; brittle. |
| `/usr/local/sap/nwrfcsdk` | Requires admin install; not VDI-friendly. |
| Static linking the SDK | SAP terms prohibit redistribution outside SAP entitlement; static linking ties license boundaries. |
| Symlinks across `lib/` | Citrix profile redirection can move only the parent; followers break. |

## 3. Windows VDI (Server 2019+, 10/11, Citrix, Horizon, WorkSpaces, AVD)

### 3.1 DLL search order (the canonical reference)

Per Microsoft's
[Dynamic-Link Library Search Order](https://learn.microsoft.com/windows/win32/dlls/dynamic-link-library-search-order),
the desktop search order with default safe-search enabled is:

1. The directory of the application executable.
2. The system directory (`C:\Windows\System32`).
3. The 16-bit system directory.
4. The Windows directory.
5. The current directory.
6. The directories listed in the PATH environment variable.

We rely exclusively on (1). The application's `.exe` ships with
its SAP DLL siblings in the same directory.

### 3.2 Layout to ship

```
deploy\
├── app.exe
├── sapnwrfc.dll            ← from SAP NW RFC SDK 7.50 PL ≥ floor
├── sapucum.dll              (or libsapucum.dll, depending on SDK build)
├── icudt50.dll
├── icuin50.dll
├── icuuc50.dll
└── (optional)
    ├── sapcrypto.dll        ← SAP CommonCryptoLib for SNC / WS TLS
    └── ...
```

The exact ICU DLL names depend on the SAP NW RFC SDK release
that shipped with the SDK ZIP. Take whatever is present in
`<SDK>\lib\*.dll` and ship all of them; the SDK references
specific ICU symbol versions, and a wrong ICU is worse than
none.

### 3.3 Why we do NOT call SetDllDirectoryW from init()

The Windows user-mode loader resolves the .exe's import table
*during process start*, before any Go code runs. By the time
`runtime.Init` (much less a `package init()`) executes, the
loader has already either succeeded — and your DLLs were found —
or has aborted the process with `ERROR_MOD_NOT_FOUND`. Calling
`SetDllDirectoryW` from `init()` is therefore too late for cgo
direct linkage.

A few side notes:

* `LoadLibraryEx` from `init()` *can* affect later
  `LoadLibrary` calls (for crypto libs that the SAP SDK loads
  lazily, like `libsapcrypto.dll`), but is not a substitute for
  packaging discipline.
* Calling `SetDllDirectoryW("")` *removes* the current
  directory from the search order, which is what you want for
  hardening; it does not help find DLLs that are not next to
  the .exe.
* Antivirus / EDR shims sometimes block first-time `LoadLibrary`
  on cgo-imported DLLs. Add the deploy directory to the
  corporate AV allowlist; do not rely on `SetDllDirectoryW` to
  unstick AV.

The library does NOT call `SetDllDirectoryW`, by design. If
you need to unlock crypto libs at runtime, do it explicitly
in your application code, not via a hidden `init()`.

### 3.4 Anti-patterns to avoid

| Don't | Why |
|---|---|
| `setx PATH "%PATH%;C:\nwrfcsdk\lib"` | User-level env survives only the current user; Citrix random-user pools wipe it. |
| Install SDK to `C:\Program Files\nwrfcsdk` | Requires admin; not VDI-friendly. |
| Registry edits to add a DLL search path | App-V / Citrix profile sandbox often virtualizes the registry; the change does not persist. |
| MSI installers that demand admin | Most VDI ops teams reject admin-required installers for non-IT software. |
| Roaming profile copy of the deploy dir | Roaming profiles cap size and corrupt the binary; pin the deploy dir to a non-roaming path. |

### 3.5 Verify at runtime (Windows)

```powershell
.\app.exe -ensure-sdk
```

Or programmatically, same as Linux (§2.3). On Windows,
`EnsureSDK()` is **diagnostic only** — by the time it runs,
the loader has already succeeded.

### 3.6 Nexus VDI packaging

For Nexus pre-GA VDI deployments, package `nwrfc.exe` next to
`vdi-node.exe` and keep SAP NW RFC SDK DLLs outside this repository. The
supported CLI probes are:

```powershell
.\nwrfc.exe --version
.\nwrfc.exe health --json
.\nwrfc.exe preflight --json
.\nwrfc.exe test-connection --json
```

`--version` works in no-SDK builds and reports `sdk_version:"no-sdk"`.
`health` and `preflight` fail explicitly when the binary was built with
`-tags nwrfc_nosdk` or when SDK DLLs are missing. `preflight` reports
`SAPNWRFC_HOME`, required header/DLL presence, process architecture, SDK
version, capability flags, and dynamic-load status. It performs a SAP ping only
when the `GORFC_TEST_*` connection variables are present; otherwise it reports
`connection:"not_configured"` and remains a packaging check.
`test-connection --json` requires both a real SDK-linked binary and complete
`GORFC_TEST_ASHOST`, `GORFC_TEST_SYSNR`, `GORFC_TEST_CLIENT`,
`GORFC_TEST_USER`, and `GORFC_TEST_PASSWD`; it is the non-destructive DEV /
Sandbox RFC ping gate. Error output is redacted against runtime secret
environment values before it is printed.

The SAP NW RFC SDK is customer-provided. Do not vendor, embed, commit, or
redistribute `sapnwrfc.dll`, `sapucum.dll`, ICU DLLs, CommonCryptoLib, SDK
headers, or SAP credentials through this project. The Nexus bootstrapper should
detect the customer-provided SDK layout and copy approved DLLs into the VDI
install directory as an operational packaging step.

### 3.7 Building the Nexus `nwrfc.exe` on the VDI

If the Windows SDK exists only on the VDI, install Go for Windows, MSYS2
MINGW64 GCC, and the Microsoft Visual C++ 2015-2022 x64 Redistributable on the
VDI, then build in place:

```powershell
$env:PATH = "C:\Program Files\Go\bin;C:\msys64\mingw64\bin;$env:PATH"
.\scripts\build-windows-sdk.ps1 `
  -SapNWRFCHome "C:\operator-secure\nwrfcsdk-windows" `
  -Output "C:\nexus-validation\nwrfc.exe" `
  -CC "C:\msys64\mingw64\bin\gcc.exe"
```

Deploy only the produced `nwrfc.exe` plus non-proprietary Nexus artifacts. At
runtime set `SAPNWRFC_HOME` to the operator-staged SDK path and prepend
`%SAPNWRFC_HOME%\lib` to the Windows service process `PATH`; do not copy SAP
DLLs into git, CI artifacts, pull requests, or public logs. A valid deployment
must pass:

```powershell
$env:SAPNWRFC_HOME = "C:\operator-secure\nwrfcsdk-windows"
$env:PATH = "$env:SAPNWRFC_HOME\lib;$env:PATH"
C:\nexus-validation\nwrfc.exe --version
C:\nexus-validation\nwrfc.exe health --json
C:\nexus-validation\nwrfc.exe preflight --json
```

Run `test-connection --json` only with DEV/Sandbox RFC coordinates supplied
through transient environment variables or a DPAPI-backed service account. Do
not place RFC passwords in command-line arguments, config JSON, shell history,
ledger files, or service logs.

## 4. Cross-compile Linux → Windows

```bash
export GOOS=windows GOARCH=amd64
export CC="zig cc -target x86_64-windows-gnu"
export CGO_ENABLED=1
export CGO_CFLAGS="-I/path/to/nwrfcsdk-windows/include"
export CGO_LDFLAGS="-L/path/to/nwrfcsdk-windows/lib"
go build -trimpath -ldflags "-s -w" -o app.exe ./cmd/myapp
```

* `zig cc` ships its own MinGW headers / linker so no Windows
  toolchain is required on the build host.
* The SDK ZIP for the *target* platform must be unpacked on
  the *build* host; `nwrfcsdk-windows` above is the unpacked
  Windows SDK.
* The resulting `app.exe` then ships per §3.2.

The CI matrix at
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml)
runs the no-SDK variant of this cross-build on every PR; the
SDK-real variant is run by the maintainer before tagging.

## 5. Operating-system service integration

### 5.1 systemd unit (Linux)

```ini
[Unit]
Description=My SAP-integrated Service
After=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/myservice/bin
ExecStart=/opt/myservice/bin/app
Restart=on-failure
# No LD_LIBRARY_PATH needed — the binary uses $ORIGIN/lib rpath.
# Drop privileges below the operator user.
User=myservice
Group=myservice

[Install]
WantedBy=multi-user.target
```

### 5.2 Windows service (sc.exe)

```cmd
sc.exe create MyService ^
    binPath= "\"C:\opt\myservice\app.exe\" -service" ^
    DisplayName= "My SAP-integrated Service" ^
    start= auto
sc.exe failure MyService reset= 86400 actions= restart/60000/restart/60000/run/0
```

The `binPath` must be an absolute path. The Windows loader
sees `C:\opt\myservice\` as the .exe directory and resolves
the SAP DLLs from there.

## 6. Verification checklist before declaring deploy done

- [ ] Binary launches without setting `PATH` / `LD_LIBRARY_PATH`.
- [ ] `nwrfc.EnsureSDK()` returns nil.
- [ ] `nwrfc.SDKVersion()` matches the documented floor in
  [INSTALL.md](INSTALL.md).
- [ ] `nwrfc.Capabilities()` reports the features the app
  actually uses (`WebSocketRFC` if the app talks WS-RFC, etc.).
- [ ] The deploy directory is in the corporate AV allowlist.
- [ ] The deploy directory is NOT inside a roaming profile or
  Citrix profile redirection scope.
- [ ] The signed-tag build of gorfc is the one shipped (cross-
  reference via `go version -m app.exe`).
- [ ] Smoke test against the target SAP system has passed in
  the same VDI image (per
  [INTEGRATION_TESTING.md](INTEGRATION_TESTING.md)).

## 7. See also

* [INSTALL.md](INSTALL.md) — quickstart per OS.
* [BUILD.md](BUILD.md) — cross-compilation details.
* [SECURITY.md](SECURITY.md) — credentials, redaction, trace
  caps, license boundaries.
* [ERRORS.md](ERRORS.md) — typed error hierarchy and retry
  semantics.
* [INTEGRATION_TESTING.md](INTEGRATION_TESTING.md) — the live
  smoke and round-trip test playbook.
