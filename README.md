# gorfc — modern Go connector for SAP NetWeaver RFC SDK (community revival)

> **Status: revival in progress.** The new modern API under `nwrfc/` is being
> designed; nothing in this repository is production-ready yet. The current
> code in [`gorfc/`](gorfc/) is the unmodified upstream `SAP-archive/gorfc`
> baseline (Apache-2.0), preserved for reference and migration.
>
> Active redesign: see [docs/PLAN.md](docs/PLAN.md).

## What this project is

A **CGO binding** over the SAP NetWeaver RFC SDK, designed to consolidate the
best of the mature wrappers in other ecosystems into a single Go-native
library:

- **SAP JCo** — protocol coverage breadth (sRFC, tRFC, qRFC, bgRFC, IDoc,
  inbound server, repository, all serialization modes, both transports).
- **node-rfc** — modern API ergonomics: cancel, per-call timeout, custom file
  system, pluggable BCD/date/time, WebSocket RFC.
- **PyRFC** — error taxonomy: 8 distinct error categories.
- **YaNco** — runtime/backend abstraction, dependency injection.
- **SapNwRfc** — POCO/struct mapping, library-presence check.
- **Ruby nwrfc** — BigDecimal preservation, FFI patterns.
- **upstream gorfc** — preserved historical baseline and CGO bridge.

…plus differentiation that **none of the existing wrappers offer**:
codegen of typed BAPI clients, mock backend for SDK-free integration tests,
OpenTelemetry/`slog` observability with built-in secret redaction, and
`context.Context` cancellation wired to `RfcCancel`.

The full feature matrix, architecture, and tiered roadmap are in
[docs/PLAN.md](docs/PLAN.md).

## What this project is not

- **Not a pure-Go RFC implementation.** Calling SAP RFC requires the
  proprietary SAP NetWeaver RFC SDK installed by the user. The protocol is
  closed, partially documented at packet level only, and SAP itself archived
  PyRFC because no maintainer could keep up with the closed SDK; a
  community pure-Go reimplementation is not viable.
- **Not a redistributor of SAP NWRFC SDK, CommonCryptoLib, sapcrypto, or any
  other proprietary SAP artifact.** Users obtain the SDK from the SAP
  Support Portal under their own SAP entitlement.
- **Not a bypass** of SAP authorization, audit, SNC, SAProuter, or network
  policy.
- **Not a replacement** for OData, SOAP, WebGUI, or Fiori automation when
  those are the correct customer-approved integration paths.
- **Not an ORM, query builder, or `database/sql` driver** for SAP.

## Tier roadmap

| Tier | Scope | Status |
|---|---|---|
| **T0** | Security remediation: remove committed credentials, fix build tag, fix C memory leaks in legacy code | 🟡 in progress |
| **T1** | Production-grade synchronous client: `Conn`, `Pool`, `Session`, ABAP types, typed errors, ctx cancel via `RfcCancel`, WebSocket RFC capability-detected, SNC, build-without-SDK | ⏳ planned |
| **T2** | Throughput, OpenTelemetry/`slog` opt-in, custom destination/server providers, custom `IniFS`, synchronous inbound RFC server, full auth set (password, SNC, x509, MYSAPSSO2, SAML/Bearer) | ⏳ planned |
| **T3** | tRFC, qRFC, bgRFC client + server, repository abstraction, CLI | ⏳ planned |
| **T4** | Codegen of typed BAPI clients, mock backend, IDoc support (separate package), HTTP/gRPC bridge, migration guide, `v1.0.0` | ⏳ planned |

See [docs/PLAN.md](docs/PLAN.md) §10 for tier deliverables and §11 for the
33-PR implementation sequence.

## Compatibility (planned)

| Component | Minimum | Recommended |
|---|---|---|
| Go | 1.23 (with `toolchain go1.25.0`) | 1.25.x |
| SAP NWRFC SDK | 7.50 PL12 | 7.50 PL18 (Dec 2025, latest) |
| Linux | x86_64, arm64 (tier-1) | latest LTS distros |
| Windows | x86_64 (tier-1) — MinGW-w64 or `zig cc` | Windows Server 2019+ |
| macOS | best-effort (tier-2) | 13+ |

The full compatibility matrix (Go × SDK PL × OS × feature) will live in
`docs/COMPATIBILITY.md` and will be capability-detected at runtime via
`RfcGetVersion`.

### Migration note: `Conn.Lock` / `Conn.Unlock`

The modern `nwrfc.Conn` API no longer exposes `Lock` or `Unlock` methods.
Callers must stop managing connection locking directly. A single `Conn`
serializes RFC calls, metadata operations, resets, session close, and close
internally; callers that need concurrent RFC work should use `Pool` instead of
sharing one connection across goroutines.

## Documentation

| Document | Purpose |
|---|---|
| [docs/PLAN.md](docs/PLAN.md) | Authoritative consolidation plan: architecture, feature matrix, modernization, public API, cgo strategy, errors, security, testing, roadmap, PR sequence, decision log |
| [docs/INTEGRATION_GUIDE.md](docs/INTEGRATION_GUIDE.md) | Consumer integration contract for SOAP-to-native RFC migration and Nexus BAPI descriptors |
| [docs/PROJECT_OBJECTIVE.md](docs/PROJECT_OBJECTIVE.md) | Scope, success criteria, license boundary |
| [docs/GORFC_REVIVAL_ASSESSMENT.md](docs/GORFC_REVIVAL_ASSESSMENT.md) | Strengths and gaps in upstream code |
| [docs/PORTING_STRATEGY.md](docs/PORTING_STRATEGY.md) | Tier-based porting strategy |
| [AGENTS.md](AGENTS.md) | Engineering rules for AI assistants and humans |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Ground rules, security expectations |
| [doc/README.md](doc/README.md) | Legacy ABAP-to-Go type mapping reference |

## Reference projects

The consolidation draws from:

- [SAP/node-rfc](https://github.com/SAP/node-rfc) — modern API reference.
- [SAP-archive/PyRFC](https://github.com/SAP-archive/PyRFC) — error taxonomy.
- [SAP JCo](https://support.sap.com/en/product/connectors/jco.html) —
  protocol coverage reference.
- [dbosoft/YaNco](https://github.com/dbosoft/YaNco) (.NET) — runtime
  abstraction.
- [huysentruitw/SapNwRfc](https://github.com/huysentruitw/SapNwRfc) (.NET)
  — POCO mapping.
- [mydoghasworms/nwrfc](https://github.com/mydoghasworms/nwrfc) (Ruby) —
  FFI patterns.
- [SAP-archive/gorfc](https://github.com/SAP-archive/gorfc) — upstream
  baseline (this fork).

## Upstream deprecation notice

The original SAP-owned `github.com/sap/gorfc` repository is no longer
maintained — see [the deprecation issue](https://github.com/SAP/gorfc/issues/42).
This community revival picks up where it left off without claiming SAP
affiliation.

![](https://img.shields.io/badge/STATUS-COMMUNITY%20REVIVAL%20%E2%80%94%20WIP-orange.svg?longCache=true&style=flat)

## Build prerequisites (planned, post-T1)

The library will wrap the SAP NetWeaver RFC SDK via cgo. Users must install:

1. **SAP NetWeaver RFC SDK 7.50 PL12 or later** (PL18 recommended), obtained
   from the [SAP Software Center](https://support.sap.com/en/product/connectors/nwrfcsdk.html)
   under your own SAP entitlement.
2. **SAP CommonCryptoLib** (or equivalent `sapcrypto`) if SNC or
   WebSocket RFC TLS is required.
3. **Go 1.25** or later.
4. Platform C toolchain — `gcc` on Linux, MinGW-w64 or
   [`zig cc`](https://ziglang.org/documentation/master/#Zig-cc-and-Zig-c-1)
   on Windows, Xcode CLI tools on macOS.

The build will be agnostic of the SDK install path through `SAPNWRFC_HOME`
and a custom `IniFS` interface. Hardcoded SDK paths in cgo directives
(present in the upstream baseline) will be removed.

A build tag `nwrfc_nosdk` will produce a stub binary that compiles without
the SDK present and fails explicitly at runtime with `ErrSDKUnavailable` —
useful for CI environments and frameworks that import the library only for
its types.

## Quickstart (planned, post-T1)

The API in [docs/PLAN.md](docs/PLAN.md) §5 will look like:

```go
import "github.com/cjordaoc/gorfc/nwrfc"

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

conn, err := nwrfc.Open(ctx, nwrfc.Params{
    AsHost: "sap.example.com",
    SysNr:  "00",
    Client: "100",
    User:   os.Getenv("SAP_USER"),
    Passwd: os.Getenv("SAP_PASS"),
    Lang:   "EN",
})
if err != nil { return err }
defer conn.Close()

type In  struct { ReqText string `rfc:"REQUTEXT"` }
type Out struct {
    EchoText string `rfc:"ECHOTEXT"`
    RespText string `rfc:"RESPTEXT"`
}

var out Out
if _, err := nwrfc.Call(ctx, conn, "STFC_CONNECTION", In{ReqText: "ping"}, &out); err != nil {
    return err
}
```

Until T1 lands, **no API is stable**. The legacy upstream code in `gorfc/`
still works against the SDK if you set `CGO_CFLAGS`/`CGO_LDFLAGS`, but is
documented to have memory-leak and silent-fallback bugs (see
[docs/GORFC_REVIVAL_ASSESSMENT.md](docs/GORFC_REVIVAL_ASSESSMENT.md) and
[docs/PLAN.md](docs/PLAN.md) §1.3).

## Lazy table streaming

Large TABLES responses can be read with `nwrfc.CallTableStream` instead of
materializing the complete table through `Call` / `CallMap`:

```go
res, err := nwrfc.CallTableStream(ctx, conn, "BAPI_MATERIAL_GETLIST", "MATNRLIST", in)
if err != nil { return err }
defer res.Close()

for {
    row, err := res.Next(ctx)
    if errors.Is(err, io.EOF) { break }
    if err != nil { return err }
    _ = row["MATERIAL"]
}
```

The stream owns the live SDK function handle. While it is open, the connection
is pinned and must not be returned to a pool. `Close` is mandatory after EOF,
early break, cancellation, or iteration error.

## Licensing

Apache-2.0, inherited from upstream. See [LICENSE](LICENSE).

The SAP NetWeaver RFC SDK and SAP CommonCryptoLib have separate SAP
licenses; users must obtain them through their own SAP entitlement. **Do
not commit SDK or CommonCryptoLib artifacts to this repository.** This
restriction is enforced by `.gitignore` patterns and CI secret scanning.

Detailed third-party component info via the [REUSE tool](https://api.reuse.software/info/github.com/SAP/gorfc).

## Background reading

For RFC concepts and runtime behavior, the SAP Professional Journal
articles remain useful:

- [Part I — RFC Client Programming](https://wiki.scn.sap.com/wiki/x/zz27Gg)
- [Part II — RFC Server Programming](https://wiki.scn.sap.com/wiki/x/9z27Gg)
- [Part III — Advanced Topics](https://wiki.scn.sap.com/wiki/x/FD67Gg)

For the SDK itself: the
[SAP NetWeaver RFC SDK Programming Guide](https://support.sap.com/en/product/connectors/nwrfcsdk.html)
is the source of truth for C API behavior. All assertions in
[docs/PLAN.md](docs/PLAN.md) marked 🟡 are pending verification against
this document.
