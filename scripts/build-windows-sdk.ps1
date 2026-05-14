<# 
Build nwrfc.exe on Windows/amd64 with the SAP NW RFC SDK.

The script never downloads, copies, vendors, or redistributes the SAP SDK. It
only points cgo at an operator-provided SAPNWRFC_HOME and writes the resulting
Go binary to the requested output path.
#>

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$SapNWRFCHome,

  [string]$Output = ".\bin\nwrfc.exe",

  [string]$Package = ".\cmd\nwrfc",

  [string]$Go = "go",

  [string]$CC = "gcc",

  [switch]$SkipPreflight
)

$ErrorActionPreference = "Stop"

function Test-RequiredFile {
  param([string]$Path, [string]$Name)
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "$Name not found at $Path"
  }
}

$sdkHome = (Resolve-Path -LiteralPath $SapNWRFCHome).Path
$includeDir = Join-Path $sdkHome "include"
$libDir = Join-Path $sdkHome "lib"

Test-RequiredFile (Join-Path $includeDir "sapnwrfc.h") "sapnwrfc.h"
Test-RequiredFile (Join-Path $libDir "sapnwrfc.dll") "sapnwrfc.dll"
Test-RequiredFile (Join-Path $libDir "libsapucum.dll") "libsapucum.dll"

foreach ($tool in @($Go, $CC)) {
  if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
    throw "required build tool not found on PATH: $tool"
  }
}

$outputDir = Split-Path -Parent $Output
if ($outputDir) {
  New-Item -ItemType Directory -Force -Path $outputDir | Out-Null
}

$oldEnv = @{
  CGO_ENABLED = $env:CGO_ENABLED
  GOOS = $env:GOOS
  GOARCH = $env:GOARCH
  CC = $env:CC
  CGO_CFLAGS = $env:CGO_CFLAGS
  CGO_LDFLAGS = $env:CGO_LDFLAGS
  SAPNWRFC_HOME = $env:SAPNWRFC_HOME
  PATH = $env:PATH
}

try {
  $env:CGO_ENABLED = "1"
  $env:GOOS = "windows"
  $env:GOARCH = "amd64"
  $env:CC = $CC
  $env:SAPNWRFC_HOME = $sdkHome
  $env:CGO_CFLAGS = "-I$includeDir"
  $env:CGO_LDFLAGS = "-L$libDir"
  if (-not (($env:PATH -split ";") | Where-Object { $_ -ieq $libDir })) {
    $env:PATH = "$libDir;$env:PATH"
  }

  & $Go build -trimpath -ldflags "-s -w" -o $Output $Package
  if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
  }

  if (-not $SkipPreflight) {
    & $Output preflight --json
    if ($LASTEXITCODE -ne 0) {
      throw "nwrfc preflight failed"
    }
  }
} finally {
  foreach ($key in $oldEnv.Keys) {
    Set-Item -Path "Env:$key" -Value $oldEnv[$key]
  }
}
