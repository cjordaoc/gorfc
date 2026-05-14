<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Integration Testing Against a Live SAP System

This document is the operator playbook for running the
SAP-backed integration suite (Categoria B in the v1.x
verification rounds). The tests in this category require:

- Network reachability to a SAP application server's dispatcher
  port (typically `32<sysnr>` for direct connections).
- A valid SAP user with `S_RFC` authorization for the function
  groups exercised by the tests (`SRFC` for `STFC_CONNECTION`
  and `STFC_STRUCTURE`).
- The SAP NetWeaver RFC SDK matching the host OS, dropped under
  `nwrfcsdk/` (gitignored).
- A C toolchain compatible with cgo on the host.

## Non-negotiables (per AGENTS.md)

- **Never commit credentials, hostnames, system IDs, or SNC
  material** into this repo. The script in
  `scripts/run-integration-tests.ps1` reads everything from
  environment variables; it never writes them to disk.
- **Never log credential values.** The script logs the *names*
  of the env vars it requires, never their values. If you fork
  the script, preserve that contract.
- **Never claim a test passed against a SAP system you did not
  actually reach.** The script's exit code (3 on test failure)
  is the source of truth.

## Required environment variables

The script expects these names. If your existing SAP
credentials are in different env vars, copy them across before
invoking — the script never reads from arbitrary names:

| Var | Purpose | Example |
|---|---|---|
| `GORFC_TEST_USER` | SAP user | `RFCUSER` |
| `GORFC_TEST_PASSWD` | password | (your password) |
| `GORFC_TEST_ASHOST` | application server host | `10.65.2.159` |
| `GORFC_TEST_SYSNR` | instance number | `00` |
| `GORFC_TEST_CLIENT` | mandant | `100` |
| `GORFC_TEST_LANG` | logon language (defaults to `EN`) | `PT` |

## The `internal/sdktest` probe package

`internal/sdktest` is **not** part of the integration suite above and is
not part of the production binding. It is an opt-in SDK probe/validation
package that exercises the SAP NetWeaver RFC SDK directly through cgo.

Its files carry the build constraint
`gorfc_sdktest && cgo && !nwrfc_nosdk`, so they do **not** compile in the
default workspace — not even when cgo is active. This is deliberate: a
contributor opening the workspace in an IDE with cgo on but without the
SAP NWRFC SDK configured would otherwise hit cascade errors from
`sapnwrfc.h`.

To build or test it you must, in addition to having the SDK configured
(`SAPNWRFC_HOME`, `CGO_CFLAGS`, `CGO_LDFLAGS`), pass the `gorfc_sdktest`
build tag explicitly:

```bash
go test -tags gorfc_sdktest ./internal/sdktest
```

## Windows VDI playbook

The repo ships a PowerShell script that automates every step
except the credential mapping (which is yours, by design):

```powershell
# 1. From your VDI shell with the VPN up, map your existing
#    SAP env vars to the names the script expects:
$env:GORFC_TEST_USER   = $env:SAP_USER       # whatever you call it
$env:GORFC_TEST_PASSWD = $env:SAP_PASSWORD
$env:GORFC_TEST_ASHOST = '10.65.2.159'
$env:GORFC_TEST_SYSNR  = '00'
$env:GORFC_TEST_CLIENT = '100'
$env:GORFC_TEST_LANG   = 'PT'

# 2. Drop the Windows NW RFC SDK ZIP under nwrfcsdk\ if you
#    have not already. The file is gitignored:
#       nwrfcsdk\nwrfc750P_18-70002755.zip

# 3. Run the playbook:
pwsh .\scripts\run-integration-tests.ps1
```

The script:

1. Verifies Go 1.23+ is in PATH.
2. Verifies a C toolchain (MinGW-w64 or `zig cc`) is available
   for cgo.
3. Extracts the SDK ZIP into `nwrfcsdk\windows\` (gitignored).
4. Sets `CGO_CFLAGS` / `CGO_LDFLAGS` against that path and adds
   the SDK `lib\` directory to `PATH` for runtime DLL loading.
5. Runs `go build ./...` to confirm cgo + SDK link cleanly.
6. Runs Categoria A behavior tests (no SAP touched).
7. Runs Categoria B smoke (`TestConnect`, `TestPing`,
   `TestConnectionAttributes`, `TestReopen`).
8. Runs `STFC_CONNECTION` + `STFC_STRUCTURE` round-trip tests.

Optional flags:

- `-SmokeOnly` — stop after step 7.
- `-SkipExtract` — reuse a previous extraction.
- `-VerboseTest` — pass `-v` to `go test`.

## Linux playbook (for reference)

The script's logic ported to bash:

```bash
# Map env vars (replace with however you store creds locally):
export GORFC_TEST_USER="$SAP_USER"
export GORFC_TEST_PASSWD="$SAP_PASSWORD"
export GORFC_TEST_ASHOST=10.65.2.159
export GORFC_TEST_SYSNR=00
export GORFC_TEST_CLIENT=100
export GORFC_TEST_LANG=PT

# Extract SDK (one-time; nwrfcsdk/linux/ is gitignored):
unzip -q nwrfcsdk/nwrfc750P_18-70002752.zip -d /tmp/nwrfc-extract
mv /tmp/nwrfc-extract/nwrfcsdk nwrfcsdk/linux
rm -rf /tmp/nwrfc-extract

# cgo paths:
export SAPNWRFC_HOME="$PWD/nwrfcsdk/linux"
export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,$SAPNWRFC_HOME/lib"
export CGO_ENABLED=1

# Smoke first:
go test -v -count=1 -run 'TestConnect$|TestPing$' ./gorfc/...
# Then STFC:
go test -v -count=1 -run 'TestConnectionEcho|TestFunctionDescription|TestTableRowAsStructure' ./gorfc/...
```

## Reading the results

A green run means:

- The cgo bindings call into the real SDK without crashing.
- `RfcOpenConnection` succeeded against your SAP system.
- `RfcPing` round-tripped a frame.
- `RfcInvoke` on `STFC_CONNECTION` echoed back the input
  payload with no UTF-8 corruption.
- `RfcInvoke` on `STFC_STRUCTURE` round-tripped a struct +
  table including FLOAT, INT, DATE, TIME, RAW, NUM, STRING.

A red run will print the exact `RFC_*` error category. The
common ones:

| Error key | Meaning | Fix |
|---|---|---|
| `RFC_LOGON_FAILURE` | wrong user/password/client/language | check `GORFC_TEST_USER` etc. |
| `RFC_COMMUNICATION_FAILURE` (`hostname unknown`) | DNS/IP wrong | check `GORFC_TEST_ASHOST` |
| `RFC_COMMUNICATION_FAILURE` (`connect rc=-2`) | port closed / VPN down | check VPN, firewall |
| `RFC_INVALID_PARAMETER` (`function not found`) | RFM does not exist on this system | usually fine for non-STFC tests on systems without the custom RFMs (e.g. `/COE/RBP_FE_DATATYPES`) |

## Categoria B — what is and is not validated

The integration suite verifies the **client** side of every
T1 / T2 binding the project owes the SDK:

| Binding family | Categoria B status with full green |
|---|---|
| `RfcOpenConnection` / `RfcCloseConnection` / `RfcPing` / `RfcGetConnectionAttributes` | ✅ verified |
| `RfcGetFunctionDesc` + descriptor traversal | ✅ verified |
| `RfcCreateFunction` / `RfcInvoke` / `RfcDestroyFunction` | ✅ verified |
| Marshal: CHAR, STRING, NUMC, INT, INT2, INT1, INT8, FLOAT, BCD, DATE, TIME, BYTE, XSTRING | ✅ verified |
| Marshal: structures + tables + nested | ✅ verified |
| `RfcSetParameterActive` (notRequested) | ✅ verified |
| Connection-level error decode (logon, comm, ABAP exception, ABAP message) | ✅ verified |

The following stay 🟡 even with a fully green Categoria B:

- **bgRFC server side** (T3.4): needs `SBGRFCCONF` configured
  on the AS ABAP plus a registered server program. Different
  network shape from client tests.
- **`RfcCancel` mid-call timing**: a separate test that needs
  a long-running RFM you can interrupt.
- **WebSocket RFC**: needs `WSPORT` exposed and `TLS_*`
  configured.
- **bgRFC unit lifecycle visible in `SBGRFCMON`**: the tests
  here exercise the client API; observability of the unit
  on the SAP side is a separate UI flow.

## Reusing the script for non-DS4 systems

The script reads only env vars; nothing is hardcoded to
`10.65.2.159` or client `100`. To run against a different SAP
system, just set different env-var values before invoking.

The Windows SDK ZIP path is hardcoded to
`nwrfcsdk\nwrfc750P_18-70002755.zip` — change the script if
your operator-supplied ZIP has a different patch number; the
extraction logic itself is patch-agnostic.
