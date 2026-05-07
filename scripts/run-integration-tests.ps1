# SPDX-FileCopyrightText: 2026 gorfc community contributors
# SPDX-License-Identifier: Apache-2.0
#
# Windows integration-test playbook for gorfc.
#
# Runs the SAP-backed integration test suite end-to-end on a
# Windows host that has:
#   - VPN / corporate network access to a SAP application server
#   - Go 1.23+ (auto-fetches go1.25 via the repo's toolchain
#     directive if missing)
#   - A C toolchain that supports cgo (MinGW-w64 in PATH, or
#     `zig cc` set as $env:CC).
#   - The repo cloned locally with the NW RFC SDK Windows ZIP
#     present in `nwrfcsdk\nwrfc750P_18-70002755.zip` (gitignored;
#     operator drops it under their own SAP entitlement).
#
# What this script does NOT do:
#   - Read or store credentials. The caller is expected to set
#     GORFC_TEST_USER / GORFC_TEST_PASSWD / GORFC_TEST_ASHOST /
#     GORFC_TEST_SYSNR / GORFC_TEST_CLIENT / GORFC_TEST_LANG
#     before invoking. AGENTS.md non-negotiable: "Do not log
#     passwords, SAP tickets, SNC material, connection strings
#     with secrets". The script never prints values, only names.
#   - Vendor or commit any SDK artifact. The Windows SDK files
#     are extracted into a gitignored subdirectory.
#
# Usage:
#
#     # 1. From your VDI shell (with VPN up + your existing
#     #    SAP credentials env vars), map them to the names
#     #    this script expects:
#     $env:GORFC_TEST_USER   = $env:SAP_USER
#     $env:GORFC_TEST_PASSWD = $env:SAP_PASSWORD
#     $env:GORFC_TEST_ASHOST = '10.65.2.159'
#     $env:GORFC_TEST_SYSNR  = '00'
#     $env:GORFC_TEST_CLIENT = '100'
#     $env:GORFC_TEST_LANG   = 'PT'
#
#     # 2. Run from the repo root:
#     pwsh .\scripts\run-integration-tests.ps1
#
#     # Optional flags:
#     #   -SmokeOnly       run only TestConnect / TestPing
#     #   -SkipExtract     reuse a previous SDK extraction
#     #   -Verbose         pass -v to go test
#
# Exit codes:
#   0  = all required tests passed
#   1  = environment problem (missing tool, missing env var)
#   2  = build failure (SDK paths or cgo)
#   3  = test failure
#

[CmdletBinding()]
param(
    [switch]$SmokeOnly,
    [switch]$SkipExtract,
    [switch]$VerboseTest
)

$ErrorActionPreference = 'Stop'

function Write-Section([string]$msg) {
    Write-Host ""
    Write-Host "==> $msg" -ForegroundColor Cyan
}

function Fail([string]$msg, [int]$code = 1) {
    Write-Host "FAIL: $msg" -ForegroundColor Red
    exit $code
}

# ---------------------------------------------------------------
# 1. Sanity: we are at the repo root.
# ---------------------------------------------------------------
Write-Section "Repo root check"
if (-not (Test-Path '.\go.mod')) {
    Fail "go.mod not found in current directory; run from the repo root."
}
if (-not (Test-Path '.\internal\sdkbackend')) {
    Fail "internal\sdkbackend not found; this does not look like the gorfc repo."
}
$repoRoot = (Get-Location).Path
Write-Host "Repo root: $repoRoot"

# ---------------------------------------------------------------
# 2. Tool checks: Go, gcc/clang, unzip-equivalent.
# ---------------------------------------------------------------
Write-Section "Tool checks"

$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    Fail "go not found in PATH. Install from https://go.dev/dl/ (need 1.23+)."
}
$goVersion = & go version
Write-Host "Go: $goVersion"

# Check go satisfies our toolchain directive (1.23+).
$matches = [regex]::Match($goVersion, 'go(\d+)\.(\d+)')
if (-not $matches.Success) {
    Fail "could not parse Go version from: $goVersion"
}
$gomaj = [int]$matches.Groups[1].Value
$gomin = [int]$matches.Groups[2].Value
if ($gomaj -lt 1 -or ($gomaj -eq 1 -and $gomin -lt 23)) {
    Fail "Go $gomaj.$gomin is too old; need 1.23+."
}

$cc = $env:CC
if (-not $cc) {
    $gccCmd = Get-Command gcc -ErrorAction SilentlyContinue
    if ($gccCmd) {
        Write-Host "Found gcc: $($gccCmd.Source)"
    }
    else {
        $zigCmd = Get-Command zig -ErrorAction SilentlyContinue
        if ($zigCmd) {
            Write-Host "Found zig at $($zigCmd.Source); will set CC=zig cc"
            $env:CC = "zig cc -target x86_64-windows-gnu"
        }
        else {
            Fail "No C compiler found. Install MinGW-w64 (https://www.mingw-w64.org/) and add it to PATH, OR install zig (https://ziglang.org/download/) and rerun."
        }
    }
}
else {
    Write-Host "Using CC from environment: $cc"
}

# ---------------------------------------------------------------
# 3. Required env vars (no values printed; only names).
# ---------------------------------------------------------------
Write-Section "Credential env vars"

$required = @(
    'GORFC_TEST_USER',
    'GORFC_TEST_PASSWD',
    'GORFC_TEST_ASHOST',
    'GORFC_TEST_SYSNR',
    'GORFC_TEST_CLIENT'
)
$missing = @()
foreach ($name in $required) {
    if (-not (Test-Path "Env:$name")) {
        $missing += $name
    }
}
if ($missing.Count -gt 0) {
    Write-Host "Missing env vars: $($missing -join ', ')" -ForegroundColor Red
    Write-Host ""
    Write-Host "Set them in your shell BEFORE invoking this script. Example:"
    Write-Host '    $env:GORFC_TEST_USER   = $env:SAP_USER'
    Write-Host '    $env:GORFC_TEST_PASSWD = $env:SAP_PASSWORD'
    Write-Host '    $env:GORFC_TEST_ASHOST = ''10.65.2.159'''
    Write-Host '    $env:GORFC_TEST_SYSNR  = ''00'''
    Write-Host '    $env:GORFC_TEST_CLIENT = ''100'''
    Write-Host '    $env:GORFC_TEST_LANG   = ''PT'''
    Fail "credential env vars not set."
}
if (-not (Test-Path 'Env:GORFC_TEST_LANG')) {
    Write-Host "GORFC_TEST_LANG not set; defaulting to 'EN'." -ForegroundColor Yellow
    $env:GORFC_TEST_LANG = 'EN'
}
Write-Host "All required env vars are set (values not printed)."

# ---------------------------------------------------------------
# 4. SDK extraction.
# ---------------------------------------------------------------
Write-Section "NW RFC SDK extraction"

$sdkZip = '.\nwrfcsdk\nwrfc750P_18-70002755.zip'
$sdkDir = '.\nwrfcsdk\windows'

if (-not (Test-Path $sdkZip)) {
    Fail "SDK ZIP not found at $sdkZip. Drop the Windows SAP NW RFC SDK ZIP under nwrfcsdk\\ (gitignored)."
}

if ($SkipExtract -and (Test-Path "$sdkDir\lib\sapnwrfc.dll")) {
    Write-Host "Reusing existing extraction at $sdkDir"
}
else {
    if (Test-Path $sdkDir) {
        Write-Host "Removing old extraction at $sdkDir"
        Remove-Item -Recurse -Force $sdkDir
    }
    New-Item -ItemType Directory -Path $sdkDir | Out-Null

    Write-Host "Extracting $sdkZip ..."
    # The ZIP contains a top-level 'nwrfcsdk' directory; extract
    # to a temp parent and move contents to $sdkDir.
    $tmp = Join-Path $env:TEMP "nwrfcsdk-extract-$([System.Guid]::NewGuid().ToString('N'))"
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Expand-Archive -Path $sdkZip -DestinationPath $tmp -Force
        $inner = Join-Path $tmp 'nwrfcsdk'
        if (-not (Test-Path $inner)) {
            Fail "expected 'nwrfcsdk' directory inside ZIP; got: $(Get-ChildItem $tmp | Select-Object -ExpandProperty Name)"
        }
        Move-Item -Path "$inner\*" -Destination $sdkDir
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    if (-not (Test-Path "$sdkDir\lib\sapnwrfc.dll")) {
        Fail "extraction did not produce $sdkDir\lib\sapnwrfc.dll. Inspect $sdkDir to debug."
    }
    Write-Host "Extracted to $sdkDir"
}

# ---------------------------------------------------------------
# 5. cgo + DLL-search setup.
# ---------------------------------------------------------------
Write-Section "cgo + runtime setup"

$sdkAbs = (Resolve-Path $sdkDir).Path
$env:SAPNWRFC_HOME = $sdkAbs
$env:CGO_CFLAGS    = "-I$sdkAbs\include"
$env:CGO_LDFLAGS   = "-L$sdkAbs\lib"
$env:CGO_ENABLED   = "1"
# Windows runtime needs the SDK DLL on PATH (no rpath equivalent).
$env:PATH = "$sdkAbs\lib;$env:PATH"

Write-Host "SAPNWRFC_HOME = $env:SAPNWRFC_HOME"
Write-Host "CGO_CFLAGS    = $env:CGO_CFLAGS"
Write-Host "CGO_LDFLAGS   = $env:CGO_LDFLAGS"
Write-Host "PATH includes $sdkAbs\lib"

# ---------------------------------------------------------------
# 6. SDK-free + cgo build sanity (fast, no SAP system needed).
# ---------------------------------------------------------------
Write-Section "Build sanity (no SAP touched yet)"

Write-Host "go build ./..."
$buildOut = & go build ./... 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host $buildOut
    Fail "go build failed; cgo + SDK paths look wrong." 2
}
Write-Host "OK"

# ---------------------------------------------------------------
# 7. Local SDK behavior tests (no SAP touched).
# ---------------------------------------------------------------
Write-Section "Local SDK behavior tests (Categoria A)"

$testFlags = @('-count=1')
if ($VerboseTest) { $testFlags += '-v' }

$paths = @('./internal/sdktest/...', './nwrfc/...')
foreach ($p in $paths) {
    Write-Host "go test $($testFlags -join ' ') -run 'TestSDK|TestLanguage|TestSetTrace|TestNewTID|TestNewUnitID|TestUnitState|TestRcAsString|TestTypeAsString|TestDirectionAsString|TestCapabilities' $p"
    & go test @testFlags '-run' 'TestSDK|TestLanguage|TestSetTrace|TestNewTID|TestNewUnitID|TestUnitState|TestRcAsString|TestTypeAsString|TestDirectionAsString|TestCapabilities' $p
    if ($LASTEXITCODE -ne 0) {
        Fail "Categoria A tests failed at $p" 3
    }
}
Write-Host "OK"

# ---------------------------------------------------------------
# 8. Live SAP smoke (Categoria B) — TestConnect / TestPing.
# ---------------------------------------------------------------
Write-Section "Live SAP smoke (Categoria B)"

Write-Host "go test $($testFlags -join ' ') -run 'TestConnect$|TestPing$|TestConnectionAttributes$|TestReopen$' ./gorfc/..."
& go test @testFlags '-run' 'TestConnect$|TestPing$|TestConnectionAttributes$|TestReopen$' ./gorfc/...
if ($LASTEXITCODE -ne 0) {
    Fail "smoke tests failed; check VPN / credentials / SAP service availability." 3
}
Write-Host "Smoke OK — connection + ping + attributes round-trip."

if ($SmokeOnly) {
    Write-Section "Done (smoke-only mode)"
    exit 0
}

# ---------------------------------------------------------------
# 9. STFC_CONNECTION + STFC_STRUCTURE round-trip.
# ---------------------------------------------------------------
Write-Section "STFC_CONNECTION + STFC_STRUCTURE round-trip"

$stfcRun = 'TestConnectionEcho|TestFunctionDescription|TestTableRowAsStructure|TestTableRowAsMap|TestConfigParameter|TestInvalidParameterFunctionCall|TestErrorFunctionCall'
Write-Host "go test $($testFlags -join ' ') -run '$stfcRun' ./gorfc/..."
& go test @testFlags '-run' $stfcRun ./gorfc/...
if ($LASTEXITCODE -ne 0) {
    Fail "STFC round-trip tests failed" 3
}
Write-Host "OK"

# ---------------------------------------------------------------
# 10. Done.
# ---------------------------------------------------------------
Write-Section "All tests passed"
Write-Host "If you want to run the full integration suite (data type"
Write-Host "edges + UTCLong + RAW), invoke:"
Write-Host '    go test -count=1 ./gorfc/...'
Write-Host "but be aware some tests reference custom RFMs (e.g."
Write-Host "/COE/RBP_FE_DATATYPES, ZDATATYPES) that may not exist on"
Write-Host "your DS4 system; those will skip or fail with"
Write-Host "RFC_INVALID_PARAMETER 'function not found'."
exit 0
