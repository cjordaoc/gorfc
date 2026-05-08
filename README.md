# gorfc — modern Go connector for SAP NetWeaver RFC SDK (community revival)

> **Status: v0.2.0 release candidate.** The new typed API
> under [`nwrfc/`](nwrfc/) covers the synchronous client,
> connection pool, transactional clients (tRFC / qRFC /
> bgRFC), redaction-by-default logs, mid-call cancellation,
> and Windows/Linux VDI deployment. The legacy upstream
> package under [`gorfc/`](gorfc/) is preserved unmodified for
> migration; new work should target `nwrfc/`.
>
> Living roadmap:
> [docs/ROADMAP_NEXUS_INTEGRATION.md](docs/ROADMAP_NEXUS_INTEGRATION.md).

![Apache-2.0](https://img.shields.io/badge/license-Apache%202.0-blue)
![Go 1.25 toolchain](https://img.shields.io/badge/go-1.25.x-00ADD8)
![SDK 7.50 PL3+ verified PL18](https://img.shields.io/badge/SAP%20NW%20RFC%20SDK-7.50%20PL3%2B%20%E2%80%94%20verified%20PL18-orange)

---

## A note on revival and gratitude

This repository is a community-maintained continuation of the
archived
[`SAP-archive/gorfc`](https://github.com/SAP-archive/gorfc)
project. The original work — the cgo plumbing, the type
mapping decisions, the test fixtures, the cross-platform
build directives — was authored at SAP and made available
under Apache-2.0. **Thank you to the original gorfc
maintainers and contributors at SAP for laying that
foundation.** Without that work, this revival would not have
a starting point.

The original upstream is no longer maintained
([deprecation notice](https://github.com/SAP/gorfc/issues/42)).
This fork picks up under a new module path,
`github.com/cjordaoc/gorfc`, and ships a redesigned typed API
under `nwrfc/` while preserving the legacy `gorfc/` package
verbatim for callers who need a one-release-cycle migration
window.

This is a **community project**. It does not claim SAP
affiliation, does not redistribute SAP binaries, and does not
ship customer-specific configuration. See
[AGENTS.md](AGENTS.md) for the engineering rules and
[docs/SECURITY.md](docs/SECURITY.md) for the security
policy.

---

## What this project is

A **typed Go binding** over the SAP NetWeaver RFC SDK,
suitable for production deployment on Linux and Windows VDI.
v0.2.0 ships:

* **Typed connection lifecycle** — `nwrfc.Conn`, `nwrfc.Pool`,
  `nwrfc.Session`. `context.Context` everywhere.
* **Mid-call cancellation** — `Conn.Cancel()` plus an automatic
  cancel watcher on every blocking op (Open / Ping / Reset /
  Describe / Invoke). Driven by `RfcCancel`; see
  [docs/EVIDENCE/sdk-cancel.md](docs/EVIDENCE/sdk-cancel.md).
* **Typed error hierarchy** — eight SDK-mapped categories,
  five Go-side types, four logon subtypes
  (`PasswordExpiredError`, `UserLockedError`,
  `InvalidCredentialsError`, `UnknownLogonFailureError`).
  No `strings.Contains` on error messages.
* **Redaction-by-default logging** — `slog.LogValuer` and
  `fmt.Stringer` on `Params` and every typed error. Single
  source of truth for the sensitive-key matcher.
* **Trace cap** — `Params.MaxTraceLevel` prevents downstream
  code from raising the SDK trace verbosity beyond a
  policy-declared ceiling.
* **WebSocket RFC capability hardlock** — using `WSHost`
  against an SDK PL that does not support WebSocket fails
  fast. No silent transport downgrade.
* **`PoolConfig.AlwaysReset`** — opt-in `RfcResetServerContext`
  before every checkout to prevent ABAP context leak between
  callers.
* **Transactional clients** — tRFC, qRFC, bgRFC client APIs
  bound to the SDK at PL18. Server-side handlers are stubbed;
  see roadmap §12.
* **Public ABAP scalar aliases** — `nwrfc.Date`, `nwrfc.Time`,
  `nwrfc.UTCLong`, `nwrfc.Decimal`. No `internal/` import
  needed for struct-tag mapping.
* **Memoized SDK probes** — `EnsureSDK`, `SDKVersion`,
  `Capabilities` use `sync.OnceValue`.
* **Build-without-SDK** — `-tags nwrfc_nosdk` produces a stub
  binary that compiles without the SAP SDK present and fails
  explicitly at runtime with `*nwrfc.SDKUnavailableError`.
* **VDI-first deployment story** — see
  [docs/DEPLOY.md](docs/DEPLOY.md) for the Linux
  `$ORIGIN`-relative rpath layout and the Windows
  `.exe`-adjacent DLL layout.

The full API design rationale is in
[docs/PLAN.md](docs/PLAN.md); the v0.2.0 cycle's living plan
is in [docs/ROADMAP_NEXUS_INTEGRATION.md](docs/ROADMAP_NEXUS_INTEGRATION.md).

---

## What this project is not

* **Not a pure-Go RFC implementation.** Calling SAP RFC
  requires the proprietary SAP NetWeaver RFC SDK installed
  separately. The protocol is closed, partially documented,
  and SAP itself archived PyRFC because no maintainer could
  keep up with the closed SDK.
* **Not a redistributor of SAP NWRFC SDK, CommonCryptoLib,
  sapcrypto, or any other proprietary SAP artifact.** Users
  obtain the SDK from the
  [SAP Software Center](https://support.sap.com/en/product/connectors/nwrfcsdk.html)
  under their own SAP entitlement.
* **Not a bypass** of SAP authorization, audit, SNC,
  SAProuter, or network policy.
* **Not an ORM, query builder, or `database/sql` driver** for
  SAP.

---

## Requirements

| Component | Minimum | Recommended | Notes |
|---|---|---|---|
| Go | 1.23 (with `toolchain go1.25.0`) | 1.25.x | `go.mod` directive auto-fetches 1.25 if absent. |
| SAP NetWeaver RFC SDK | 7.50 PL3 | 7.50 PL18 (Dec 2025) | Obtain from SAP Software Center under your own entitlement. WebSocket RFC requires PL10+; bgRFC requires PL5+. |
| C toolchain | `gcc` on Linux, MinGW-w64 or `zig cc` on Windows, Xcode CLI tools on macOS | Same | Only required for SDK-linked builds; `nwrfc_nosdk` builds don't need cgo. |
| (Optional) SAP CommonCryptoLib | latest from SAP Support | Same | Required only for SNC or WebSocket-RFC TLS. |

This project does NOT redistribute the SDK or
CommonCryptoLib. See [docs/SECURITY.md](docs/SECURITY.md) §7
for the license boundary.

---

## Installation

```bash
go get github.com/cjordaoc/gorfc/nwrfc@v0.2.0
```

For the build configuration (where to put the SDK; what to
set `CGO_CFLAGS` / `CGO_LDFLAGS` to), see:

* [docs/INSTALL.md](docs/INSTALL.md) — quickstart per OS.
* [docs/BUILD.md](docs/BUILD.md) — cross-compilation, build
  tags, IDE configuration.
* [docs/DEPLOY.md](docs/DEPLOY.md) — VDI / production
  deployment with `$ORIGIN`-relative rpath (Linux) and
  `.exe`-adjacent DLLs (Windows).

### Build modes

| Mode | Tag | What you get |
|---|---|---|
| Default | (none) | cgo backend; full RFC functionality. Requires SDK headers + libs at build time. |
| SDK-free | `-tags nwrfc_nosdk` | No-SDK stub. Every operation returns `*nwrfc.SDKUnavailableError`. Useful for CI and downstream packages that re-export `nwrfc` types but don't connect to SAP. |
| No cgo | `CGO_ENABLED=0` | Same as `-tags nwrfc_nosdk`; the no-SDK stub is the only valid choice when cgo is off. |

---

## Quickstart

```go
package main

import (
    "context"
    "log"
    "log/slog"
    "os"
    "time"

    "github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
    // Fail-fast at process start if the SDK is missing.
    if err := nwrfc.EnsureSDK(); err != nil {
        log.Fatalf("nwrfc: %v", err)
    }
    log.Printf("nwrfc: SDK %s; capabilities=%+v",
        nwrfc.SDKVersion(), nwrfc.Capabilities())

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    conn, err := nwrfc.Open(ctx, nwrfc.Params{
        AsHost: "sap.example.com",
        SysNr:  "00",
        Client: "100",
        User:   os.Getenv("SAP_USER"),
        Passwd: os.Getenv("SAP_PASS"),
        Lang:   "EN",
        // Cap any downstream SetTraceLevel — trace > 0 captures payloads.
        MaxTraceLevel: 1,
    })
    if err != nil {
        // The error is typed; branch on category, never on string.
        slog.Error("rfc open failed", "err", err)
        os.Exit(1)
    }
    defer conn.Close()

    type In struct {
        ReqText string `rfc:"REQUTEXT"`
    }
    type Out struct {
        EchoText string `rfc:"ECHOTEXT"`
        RespText string `rfc:"RESPTEXT"`
    }

    var out Out
    if _, err := nwrfc.Call(ctx, conn, "STFC_CONNECTION",
        In{ReqText: "ping"}, &out); err != nil {
        slog.Error("call failed", "err", err)
        os.Exit(1)
    }
    log.Printf("echo=%q resp=%q", out.EchoText, out.RespText)
}
```

### Connection pool

```go
pool, err := nwrfc.NewPool(nwrfc.PoolConfig{
    Params:      nwrfc.Params{ /* ... */ },
    MinSize:     2,
    MaxSize:     8,
    IdleTimeout: 5 * time.Minute,
    AlwaysReset: true, // RfcResetServerContext before every checkout
})
if err != nil { log.Fatal(err) }
defer pool.Close()

err = pool.Do(ctx, func(c *nwrfc.Conn) error {
    _, err := nwrfc.Call(ctx, c, "STFC_PING", nil, nil)
    return err
})
```

### Typed error handling

```go
err := nwrfc.Open(ctx, p)
switch {
case errors.Is(err, nwrfc.ErrPasswordExpired):
    // surface "your password expired" UX
case errors.Is(err, nwrfc.ErrUserLocked):
    // escalate to admin (do NOT retry)
case errors.Is(err, nwrfc.ErrInvalidCredentials):
    // re-prompt the human
case errors.Is(err, nwrfc.ErrLogon):
    // generic logon fallback (still NOT a comm error)
case errors.Is(err, nwrfc.ErrCommunication):
    // network / gateway issue — IsRetryable() returns true
}
```

See [docs/ERRORS.md](docs/ERRORS.md) for the full taxonomy
and the **mid-call cancellation caveat** for mutating BAPIs.

---

## Documentation

| Document | Purpose |
|---|---|
| [docs/INSTALL.md](docs/INSTALL.md) | Quickstart per OS. |
| [docs/BUILD.md](docs/BUILD.md) | Cross-compilation, build tags, IDE config. |
| [docs/DEPLOY.md](docs/DEPLOY.md) | VDI / production deployment playbook. |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | Runtime configuration (Params, IniFS, providers). |
| [docs/ERRORS.md](docs/ERRORS.md) | Typed error hierarchy, retry semantics, cancellation caveat. |
| [docs/SECURITY.md](docs/SECURITY.md) | Credentials, redaction, trace cap, license boundary. |
| [docs/INTEGRATION_TESTING.md](docs/INTEGRATION_TESTING.md) | Categoria-B SAP-real test playbook. |
| [docs/EVIDENCE/SCHEMA.md](docs/EVIDENCE/SCHEMA.md) | Evidence file schema for SAP-real test runs. |
| [docs/MIGRATION_FROM_GORFC.md](docs/MIGRATION_FROM_GORFC.md) | Mechanical port from upstream `gorfc`. |
| [docs/ROADMAP_NEXUS_INTEGRATION.md](docs/ROADMAP_NEXUS_INTEGRATION.md) | v0.2.0 living roadmap. |
| [docs/PLAN.md](docs/PLAN.md) | Authoritative consolidation plan. |
| [docs/PROJECT_OBJECTIVE.md](docs/PROJECT_OBJECTIVE.md) | Scope, success criteria, license boundary. |
| [docs/SDK_FUNCTIONS_MAP.md](docs/SDK_FUNCTIONS_MAP.md) | Every SDK function used, with verification status. |
| [AGENTS.md](AGENTS.md) | Engineering rules for AI assistants and humans. |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Ground rules and the maintainer signed-tag procedure. |
| [CHANGES](CHANGES) | Per-release changelog. |

---

## Compatibility & tier roadmap

| Tier | Scope | Status |
|---|---|---|
| **T0** | Security remediation: removed committed credentials, fixed build tags, fixed C memory leaks in legacy code | ✅ landed |
| **T1** | Synchronous client: `Conn`, `Pool`, `Session`, ABAP types, typed errors, ctx cancel via `RfcCancel`, WebSocket RFC capability-detected, SNC, build-without-SDK | ✅ landed |
| **T2** | Throughput, OpenTelemetry/`slog` opt-in, custom destination/server providers, custom `IniFS`, full auth set | 🟡 partial |
| **T3** | tRFC, qRFC, bgRFC client + server, repository abstraction, CLI | 🟡 client landed; server stubbed |
| **T4** | Codegen of typed BAPI clients, mock backend, IDoc support, HTTP/gRPC bridge, migration guide, `v1.0.0` | 🟡 partial |
| **v0.2.0** | nexus-spec integration hardening: redaction, mid-call cancel, logon subtypes, VDI deploy, Windows CI, evidence schema, trace cap | ✅ landed (this release) |

See [docs/PLAN.md](docs/PLAN.md) §10 for tier deliverables.

---

## Reference projects

The consolidation draws from:

* [SAP/node-rfc](https://github.com/SAP/node-rfc) — modern API
  reference.
* [SAP-archive/PyRFC](https://github.com/SAP-archive/PyRFC) —
  error taxonomy.
* [SAP JCo](https://support.sap.com/en/product/connectors/jco.html)
  — protocol coverage reference.
* [dbosoft/YaNco](https://github.com/dbosoft/YaNco) (.NET) —
  runtime abstraction.
* [huysentruitw/SapNwRfc](https://github.com/huysentruitw/SapNwRfc)
  (.NET) — POCO mapping.
* [mydoghasworms/nwrfc](https://github.com/mydoghasworms/nwrfc)
  (Ruby) — FFI patterns.
* [SAP-archive/gorfc](https://github.com/SAP-archive/gorfc) —
  upstream baseline (this fork's starting point; gratitude
  again).

---

## Licensing

Apache-2.0, inherited from upstream. See [LICENSE](LICENSE).

The SAP NetWeaver RFC SDK and SAP CommonCryptoLib have
separate SAP licenses; users obtain them through their own SAP
entitlement. **Do not commit SDK or CommonCryptoLib artifacts
to this repository.** This restriction is enforced by
`.gitignore` patterns and the `secret-scan` GH workflow.

Detailed third-party component info via the
[REUSE tool](https://api.reuse.software/info/github.com/SAP/gorfc).

---

## Background reading

For RFC concepts and runtime behavior, the SAP Professional
Journal articles remain useful:

* [Part I — RFC Client Programming](https://wiki.scn.sap.com/wiki/x/zz27Gg)
* [Part II — RFC Server Programming](https://wiki.scn.sap.com/wiki/x/9z27Gg)
* [Part III — Advanced Topics](https://wiki.scn.sap.com/wiki/x/FD67Gg)

For the SDK itself: the
[SAP NetWeaver RFC SDK Programming Guide](https://support.sap.com/en/product/connectors/nwrfcsdk.html)
is the source of truth for C API behavior. Assertions in
[docs/PLAN.md](docs/PLAN.md) marked 🟡 are pending verification
against this document; verified ones are flagged ✅.
