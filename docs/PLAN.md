# Plano Técnico de Consolidação — Wrapper Go para SAP NetWeaver RFC SDK

> Documento canônico do projeto. Sob [AGENTS.md](../AGENTS.md). Sem implementação.
>
> Marcações de verificação:
> - ✅ **Confirmado por fonte primária** (Go release notes, SAP KBA público, header SDK confirmado)
> - 🔵 **Inferido** de docs de wrappers de referência (node-rfc, PyRFC, JCo, YaNco)
> - 🟡 **Requires SDK verification** (símbolo C, comportamento, ou versão mínima precisa ser checado no SDK PL18 instalado)
> - 🔴 **Requires SAP runtime verification** (precisa instância SAP de teste para validar)

---

## Sumário

1. [Executive Summary](#1-executive-summary)
2. [Target Architecture](#2-target-architecture)
3. [Consolidation Matrix](#3-consolidation-matrix)
4. [Go Modernization Plan](#4-go-modernization-plan)
5. [Public API Proposal](#5-public-api-proposal)
6. [Internal cgo Binding Strategy](#6-internal-cgo-binding-strategy)
7. [Error Taxonomy](#7-error-taxonomy)
8. [Security Model](#8-security-model)
9. [Testing and Conformance Strategy](#9-testing-and-conformance-strategy)
10. [Roadmap by Tiers](#10-roadmap-by-tiers)
11. [Implementation Sequence](#11-implementation-sequence)
12. [Decision Log](#12-decision-log)
13. [Documentation Plan](#13-documentation-plan)
14. [Final Recommendation](#14-final-recommendation)

---

## 1. Executive Summary

Este plano consolida o melhor de seis wrappers maduros (JCo, node-rfc, PyRFC, YaNco, SapNwRfc, Ruby nwrfc) em um único wrapper Go superior em escopo e ergonomia. A decisão estratégica é **wrapping completo do SAP NetWeaver RFC SDK** — não há tentativa de reverse-engineer do protocolo. O esforço total é **18–27 pessoa-meses** distribuídos em quatro tiers de entrega, com cliente produtivo entregue em T1 (4–6 PM) e paridade JCo+node-rfc em T1+T2+T3 (18–27 PM).

A arquitetura separa rigidamente **API pública pure-Go** (sem `import "C"` em qualquer assinatura exposta) de **backend interno** (CGO + SDK) atrás de uma `Backend` interface, permitindo: (1) compilar o binário sem SDK presente (`-tags nwrfc_nosdk`); (2) plugar futuros backends (mock para testes; pure-Go subset experimental, se algum dia justificável); (3) consumir a lib em frameworks Go sem arrastar CGO obrigatório no caminho de imports não-RFC.

Diferenciação Go-native sobre os wrappers existentes:

- `context.Context` em todas as bordas com `RfcCancel` no ctx-done.
- Hierarquia tipada de erros baseada em `errors.Is/As` agregando o melhor de PyRFC + JCo + node-rfc.
- `slog` com redação automática de credenciais; OpenTelemetry opt-in via subpacote `nwrfcotel`.
- Marshaling tipado via tags `rfc:""`, com fallback dinâmico via `map[string]any`.
- Codegen de cliente BAPI tipado a partir de `FunctionDescription` SAP (Tier 4) — feature que **nenhum wrapper estudado oferece**.
- Mock backend pure-Go (Tier 4) — testes de integração rodam sem SDK.
- Suporte a WebSocket RFC com TLS, capability-detected.

Riscos não-negociáveis tratados desde T0:

- Remoção imediata de credenciais comprometidas em `gorfc/sapnwrfc.ini`.
- Correção do build tag que falsamente habilita Windows.
- Correção dos `defer C.free` que vazam memória C real em `gorfc/gorfc.go:187-188`.

---

## 2. Target Architecture

### 2.1 Layout de módulo

```
github.com/cjordaoc/gorfc                    ← module root
│
├── nwrfc/                                   ← API pública pure-Go (não importa "C")
│   ├── nwrfc.go                             ← surface: Conn, Pool, ABAP types
│   ├── conn.go                              ← Conn lifecycle
│   ├── params.go                            ← typed Params builder
│   ├── pool.go                              ← Pool + PoolConfig + Stats
│   ├── session.go                           ← Stateful session (begin/commit/rollback)
│   ├── call.go                              ← Call + CallMap + CallOptions
│   ├── metadata.go                          ← FunctionDescription, TypeDescription, Repository
│   ├── transaction.go                       ← tRFC: BeginTransaction/Submit/Confirm
│   ├── queue.go                             ← qRFC: queue name on transaction
│   ├── unit.go                              ← bgRFC: BeginUnit/Submit/Confirm + handlers
│   ├── server.go                            ← Server: Register, Start, Stop
│   ├── throughput.go                        ← Throughput stats
│   ├── attrs.go                             ← ConnectionAttributes
│   ├── types.go                             ← Date, Time, UTCLong, Decimal interface
│   ├── tags.go                              ← struct tag parsing
│   ├── errors.go                            ← typed error hierarchy
│   ├── policy.go                            ← BCDPolicy, DatePolicy, TimePolicy
│   ├── lang.go                              ← LanguageISOToSAP, LanguageSAPToISO
│   ├── trace.go                             ← Trace, TraceLevel, TraceDir
│   ├── crypto.go                            ← LoadCryptoLibrary
│   ├── ini.go                               ← SetIniPath, ReloadIniFile, IniFS
│   ├── version.go                           ← LibraryVersion, Capabilities
│   └── doc.go                               ← package overview
│
├── nwrfcparam/                              ← helpers sem CGO
│   ├── bapi.go                              ← BAPIRet2, ToBAPIErrors, AsError
│   ├── builder.go                           ← typed parameter builder
│   └── range.go                             ← SAP RANGE table helpers
│
├── nwrfcotel/                               ← OpenTelemetry opt-in (sem CGO)
│   ├── tracer.go                            ← span instrumentation
│   ├── meter.go                             ← throughput → metrics
│   └── slog.go                              ← otelslog bridge wiring
│
├── nwrfcmock/                               ← mock backend (sem CGO)
│   ├── backend.go                           ← implements internal/backend.Backend
│   ├── recorder.go                          ← record/replay fixtures
│   └── handlers.go                          ← programmable handlers
│
├── nwrfcidoc/                               ← IDoc helpers (Tier 4, sem CGO no início)
│   ├── idoc.go                              ← parse/build IDoc segments
│   └── control.go                           ← EDIDC/EDIDD record helpers
│
├── cmd/
│   ├── nwrfc/                               ← CLI: ping, call, describe, version
│   └── nwrfc-gen/                           ← codegen Tier 4
│
├── internal/
│   ├── backend/                             ← Backend interface (sem CGO)
│   │   ├── backend.go                       ← interface contract
│   │   └── registry.go                      ← name→Backend registration
│   ├── sdkbackend/                          ← CGO backend (build:cgo,!nwrfc_nosdk)
│   │   ├── bridge.go                        ← #cgo directives consolidated
│   │   ├── conn.go                          ← Open/Close/Ping
│   │   ├── invoke.go                        ← Invoke/Cancel
│   │   ├── fill.go                          ← Go→ABAP marshaling
│   │   ├── wrap.go                          ← ABAP→Go unmarshaling
│   │   ├── ucs2.go                          ← UTF-8↔SAP_UC
│   │   ├── handle.go                        ← typed handle wrappers
│   │   ├── server.go                        ← server callbacks bridge
│   │   ├── unit.go                          ← bgRFC bindings
│   │   ├── txn.go                           ← tRFC/qRFC bindings
│   │   ├── meta.go                          ← metadata bindings
│   │   ├── errors.go                        ← RFC_ERROR_INFO → typed errors
│   │   ├── caps.go                          ← runtime capability detection
│   │   └── doc.go
│   ├── nosdkbackend/                        ← stub (build:!cgo or nwrfc_nosdk)
│   │   └── stub.go                          ← all methods → ErrSDKUnavailable
│   ├── ucs2/                                ← UTF-8↔UTF-16 testable sem SDK
│   │   ├── convert.go
│   │   └── convert_test.go
│   ├── bcd/                                 ← BCD parsing testable sem SDK
│   ├── secrets/                             ← redaction policy
│   └── timeext/                             ← ABAP DATS/TIMS/UTCLONG validators
│
├── examples/                                ← rodáveis (cgo) ou compiláveis (nosdk)
│   ├── ping/
│   ├── stfc_connection/
│   ├── stfc_structure/
│   ├── bapi_user_get_detail/
│   ├── pool/
│   ├── session/
│   ├── trfc/
│   ├── bgrfc/
│   ├── server/
│   ├── otel/
│   └── mock/
│
├── docs/
│   ├── PROJECT_OBJECTIVE.md
│   ├── GORFC_REVIVAL_ASSESSMENT.md
│   ├── PORTING_STRATEGY.md
│   ├── PLAN.md                              ← este documento
│   ├── FEATURE_MATRIX.md
│   ├── SDK_FUNCTIONS_MAP.md
│   ├── ROADMAP.md
│   ├── ARCHITECTURE.md
│   ├── BUILD.md
│   ├── INSTALL.md
│   ├── CONFIGURATION.md
│   ├── SECURITY.md
│   ├── ERRORS.md
│   ├── TESTING.md
│   ├── COMPATIBILITY.md
│   ├── FEASIBILITY.md
│   └── MIGRATION_FROM_GORFC.md
│
├── compat/                                  ← shim opcional para gorfc.* legado, v0.x
│   └── gorfc/
│
├── gorfc/                                   ← código atual; remoção planejada após v1
└── doc/                                     ← legado upstream; mover para docs/legacy/
```

### 2.2 Fronteira API pública vs internal

**Regra dura:**

- Nada em `nwrfc/`, `nwrfcparam/`, `nwrfcotel/`, `nwrfcmock/`, `nwrfcidoc/` pode usar `import "C"`.
- CGO está confinado em `internal/sdkbackend/`.
- A interface `internal/backend.Backend` define o contrato em tipos pure-Go.
- Todo handle SDK (`RFC_CONNECTION_HANDLE`, `RFC_FUNCTION_HANDLE`, `RFC_TRANSACTION_HANDLE`, `RFC_UNIT_HANDLE`, `RFC_SERVER_HANDLE`) é encapsulado num tipo opaco em `internal/sdkbackend/handle.go` e nunca cruza para `nwrfc/`.

**Linkagem do backend:**

- Build tag `nwrfc_sdk` (default em CGO ambientes) → registra `sdkbackend`.
- Build tag `nwrfc_nosdk` (ou `!cgo`) → registra `nosdkbackend`.
- `nwrfcmock.Register(t, mock)` registra mock em runtime, sobrepondo o default — útil em tests.

**Contrato `Backend` (esboço, não código final):**

```
type Backend interface {
    LibraryVersion() (Version, error)
    Capabilities() Capabilities                    // detectada via probe ou versão
    Open(ctx, Params) (ConnHandle, error)
    Close(ConnHandle) error
    Ping(ctx, ConnHandle) error
    Attributes(ctx, ConnHandle) (Attributes, error)
    Describe(ctx, ConnHandle, name string) (FunctionDescription, error)
    InvalidateMetadata(ConnHandle, name string) error
    Invoke(ctx, ConnHandle, name string, in any, out any, opts CallOptions) error
    ResetServerContext(ctx, ConnHandle) error
    BeginTransaction(ctx, ConnHandle, opts TxnOptions) (TxnHandle, error)
    InvokeInTransaction(ctx, TxnHandle, name string, in any, opts CallOptions) error
    SubmitTransaction(ctx, TxnHandle) error
    ConfirmTransaction(ctx, TxnHandle) error
    DestroyTransaction(TxnHandle) error
    BeginUnit(ctx, ConnHandle, opts UnitOptions) (UnitHandle, error)
    InvokeInUnit(ctx, UnitHandle, name string, in any, opts CallOptions) error
    SubmitUnit(ctx, UnitHandle) error
    ConfirmUnit(ctx, UnitHandle) error
    GetUnitState(ctx, ConnHandle, unitID UnitID) (UnitState, error)
    StartServer(ctx, ServerConfig, registry HandlerRegistry) (ServerHandle, error)
    StopServer(ctx, ServerHandle) error
    AttachThroughput(ConnHandle, ThroughputHandle) error
    NewThroughput() (ThroughputHandle, error)
    ReadThroughput(ThroughputHandle) (ThroughputStats, error)
    DestroyThroughput(ThroughputHandle) error
    LanguageISOToSAP(string) (string, error)
    LanguageSAPToISO(string) (string, error)
    SetTraceLevel(ConnHandle, TraceLevel) error
    SetTraceDir(string) error
    SetTraceEncoding(string) error
    LoadCryptoLibrary(path string) error
    SetIniPath(path string) error
    ReloadIniFile() error
    SetIniFS(fs IniFS) error                        // node-rfc customFs analogue
    Cancel(ConnHandle) error                        // RfcCancel
}
```

Comentário de fronteira: `ConnHandle`, `TxnHandle`, `UnitHandle`, `ServerHandle`, `ThroughputHandle` são `interface{ unsafePointer() }`-equivalentes, opacos. A camada `nwrfc/` nunca chama `unsafePointer()` — só passa adiante.

### 2.3 Diagrama lógico

```
   ┌────────────────────────────────────────────┐
   │  user code (services, microservices, CLI)  │
   └──────────────┬─────────────────────────────┘
                  │ pure-Go API
                  ▼
   ┌────────────────────────────────────────────┐
   │  nwrfc/  +  nwrfcparam/  +  nwrfcidoc/     │
   │  (no CGO; safe to import everywhere)        │
   └──────────────┬─────────────────────────────┘
                  │ Backend interface
   ┌──────────────┼─────────────┬───────────────┐
   ▼              ▼             ▼               ▼
┌────────┐  ┌──────────┐  ┌──────────┐  ┌────────────────┐
│sdkback │  │nosdkback │  │mockback  │  │puregoexp (T4+) │
│(CGO)   │  │(stub)    │  │(test)    │  │(experimental)  │
└────┬───┘  └──────────┘  └──────────┘  └────────────────┘
     │ #include <sapnwrfc.h>
     ▼
┌──────────────────┐
│ libsapnwrfc.{so,dll} │
│ libsapucum.{so,dll}  │
│ libsapcrypto.*       │
└──────────────────┘
```

---

## 3. Consolidation Matrix

> Tier T1=v1 MVP · T2=v1 GA · T3=v1.1 · T4=v1.2/post.
> Inspiração: J=JCo, N=node-rfc, P=PyRFC, Y=YaNco, S=SapNwRfc, R=Ruby nwrfc, G=gorfc.

| # | Feature | Inspiração | gorfc atual | Alvo | Tier | SDK funcs (🟡 verify) | Risco | Critério de aceite | SAP real? | SDK ≥ |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | sRFC client | J/N/P/G | ✅ | ✅ | T1 | `RfcCreateFunction`, `RfcInvoke`, `RfcDestroyFunction` | baixo | Call `RFC_PING` retorna sem erro | 🔴 | 7.50 PL3 |
| 2 | Direct connection | J/N/P/G | ✅ | ✅ | T1 | `RfcOpenConnection` | baixo | Open com `ASHOST/SYSNR` retorna `*Conn` | 🔴 | 7.50 PL3 |
| 3 | Load-balanced (mshost/group) | J/N/P | 🔶 passthrough | ✅ | T1 | `RfcOpenConnection` (`MSHOST`,`GROUP`,`R3NAME`) | baixo | Connect via msg server | 🔴 | 7.50 PL3 |
| 4 | sapnwrfc.ini destination | J/N/P/G | ✅ | ✅ | T1 | `RfcOpenConnection`(`DEST`), `RfcSetIniPath` | baixo | `OpenDest("DEV")` lê ini e conecta | 🔴 | 7.50 PL3 |
| 5 | Custom destination provider | J/Y | ❌ | ✅ | T2 | — (Go-side) | médio | `Provider` interface fornece `Params` por nome | ❌ | n/a |
| 6 | Custom server provider | J | ❌ | ✅ | T3 | — | médio | `ServerProvider.GetConfig(name)` | ❌ | n/a |
| 7 | Connection pool | J/N/P/Y/S | ❌ | ✅ | T1 | reuso de Conn | médio | Pool sob carga não corrompe handles; race detector clean | 🔴 | n/a |
| 8 | Stateful session | J(`JCoContext`)/N/P | implícito | ✅ explícito | T1 | mesmo Conn; `BAPI_TRANSACTION_COMMIT/ROLLBACK` | baixo | 2 calls dentro de `Session` veem mesma `LUW` ABAP | 🔴 | 7.50 PL3 |
| 9 | Inbound RFC server (sync) | J/N/P/Y/S/R | ❌ | ✅ | T2 | `RfcRegisterServer`,`RfcInstallGenericServerFunction`,`RfcInstallServerFunction`,`RfcStartOrResumeServer`,`RfcShutdownServer` 🟡 | alto | Server registra em gateway; ABAP `CALL FUNCTION DEST` invoca handler Go | 🔴 | 7.50 PL3 |
| 10 | tRFC client | J/N/P/Y/R | ❌ | ✅ | T3 | `RfcCreateTransaction`,`RfcInvokeInTransaction`,`RfcSubmitTransaction`,`RfcConfirmTransaction`,`RfcDestroyTransaction`,`RfcGetTransactionID` 🟡 | médio | TID gerado, `Submit` enfileira, `Confirm` limpa | 🔴 | 7.50 PL3 |
| 11 | tRFC server | J/N/P | ❌ | ✅ | T3 | `RfcInstallTransactionHandlers` 🟡 | alto | Handlers `OnCheck/OnCommit/OnRollback/OnConfirm` chamados | 🔴 | 7.50 PL3 |
| 12 | qRFC client | J/N/P/Y/R | ❌ | ✅ | T3 | tRFC API + `RfcSetQueueName` 🟡 | médio | Unidade entra em fila ABAP `SMQ1` | 🔴 | 7.50 PL3 |
| 13 | qRFC server | J/N/P | ❌ | ✅ | T3 | tRFC server + queue handlers 🟡 | alto | ABAP envia em ordem por queue name | 🔴 | 7.50 PL3 |
| 14 | bgRFC client | J/N | ❌ | ✅ | T3 | `RfcCreateUnit`,`RfcInvokeInUnit`,`RfcSubmitUnit`,`RfcConfirmUnit`,`RfcGetUnitState`,`RfcDestroyUnit` ✅ confirmado | alto | Unit submetido visível em `SBGRFCMON` | 🔴 | 7.50 PL5+ 🟡 |
| 15 | bgRFC server | J/N/P | ❌ | ✅ | T3 | `RfcInstallBgRfcHandlers` 🟡 | alto | Handlers `onCheck/onCommit/onConfirm/onRollback/onGetState` invocados | 🔴 | 7.50 PL5+ 🟡 |
| 16 | IDoc | J(JCoIDoc) | ❌ | ✅ | T4 | sapidoc lib separada da SAP 🟡 — ou parser próprio sobre RFC `IDOC_INBOUND_ASYNCHRONOUS` | médio-alto | IDoc parse/build de tipo MATMAS05 | 🔴 | independente |
| 17 | Function metadata describe | J/N/P/Y/S/R/G | ✅ | ✅ | T1 | `RfcGetFunctionDesc`,`RfcGetParameterCount`,`RfcGetParameterDescByIndex`,`RfcGetTypeName`,`RfcGetFieldCount`,`RfcGetFieldDescByIndex` ✅ | baixo | `Describe("BAPI_USER_GET_DETAIL")` retorna struct completa | 🔴 | 7.50 PL3 |
| 18 | Type metadata describe | J/N/P/Y/S/G | ✅ | ✅ | T1 | acima + `RfcGetTypeLength` | baixo | `TypeDescription` recursivo em campos struct | 🔴 | 7.50 PL3 |
| 19 | Metadata cache + invalidate | J/N/P | SDK-driven | ✅ | T2 | `RfcAddFunctionDesc`,`RfcRemoveFunctionDesc` 🟡 | médio | `InvalidateMetadata("BAPI_X")` força re-fetch | 🔴 | 7.50 PL3 |
| 20 | Repository abstraction | J(JCoRepository) | ❌ | ✅ | T3 | composição sobre cache | médio | Repo compartilhado entre Conns; pré-carga + snapshot | 🔴 | n/a |
| 21 | Throughput | J/N/P | ❌ | ✅ | T2 | `RfcCreateThroughput`,`RfcSetThroughputOnConnection`,`RfcGetThroughputXxx`,`RfcDestroyThroughput` 🟡 | baixo | Stats: calls, bytes, app/total/serialization time | 🔴 | 7.53+ 🟡 |
| 22 | Auth: password | todos | ✅ | ✅ | T1 | params `USER`,`PASSWD` | baixo | Logon E sucesso | 🔴 | 7.50 PL3 |
| 23 | Auth: SSO ticket MYSAPSSO2 | J/N/P/S | ❌ | ✅ | T2 | param `MYSAPSSO2` | médio | Logon com ticket válido | 🔴 | 7.50 PL3 |
| 24 | Auth: x509 | J/N/Y/S | ❌ | ✅ | T2 | params `X509CERT` ou `TLS_CLIENT_CERTIFICATE_LOGON` | médio | Logon com cert; verify SNC ou WS | 🔴 | 7.50 PL3 |
| 25 | Auth: SNC | J/N/P/Y/S | passthrough | ✅ | T1 | params `SNC_QOP`,`SNC_LIB`,`SNC_MYNAME`,`SNC_PARTNERNAME` | médio | Logon SNC com PSE | 🔴 | 7.50 PL3 |
| 26 | Auth: SAML/Bearer | J/N/P | ❌ | ✅ | T3 | param `SAML2`/`BEARER` 🟡 | médio | Logon com bearer JWT | 🔴 | 7.50 PL12+ 🟡 |
| 27 | Transport: WebSocket RFC | J/N | ❌ | ✅ | T1 | `RfcOpenConnection` com `WSHOST`,`WSPORT`,`TLS_*`,`TLS_SAPCRYPTOLIB` | médio | Connect ws://host:443 + TLS | 🔴 | 7.50 PL10+ 🟡 |
| 28 | Transport: CPIC clássico | todos | ✅ | ✅ | T1 | default `RfcOpenConnection` | baixo | Connect via gateway 33xx | 🔴 | 7.50 PL3 |
| 29 | Cancel mid-call via context | N/Y(parc J/P) | ❌ | ✅ | T1 | `RfcCancel` 🟡 | médio | `ctx.Cancel()` durante Invoke causa CallCancelled error em ≤ 1s | 🔴 | 7.50 PL3 |
| 30 | Per-call timeout | J/N | ❌ | ✅ | T1 | `context.WithTimeout` + `RfcCancel` | médio | Timeout 5s em RFM longo: erro retornado | 🔴 | n/a |
| 31 | notRequested filter | J/N | ❌ | ✅ | T1 | `RfcSetParameterActive` 🟡 | baixo | params em notRequested não chegam ao caller | 🔴 | 7.50 PL3 |
| 32 | Direction filters (IMPORT/EXPORT/CHANGING/TABLES/RETURN) | J/N/P/Y/S/R/G | 🔶 | ✅ | T1 | iteração `RfcGetParameterDescByIndex` + `paramDesc.direction` | baixo | `DirectionFilter` parametriza `wrapResult` | ❌ | n/a |
| 33 | BCD/decimal policy | J/N/P/Y/S/R | 🔶 | ✅ | T1 | RfcGet/SetString para BCD; conv Go-side | médio | string default; opt-in `Decimal` interface (apd/shopspring) | 🔴 | 7.50 PL3 |
| 34 | Date/time policy | todos | 🔶 | ✅ | T1 | RfcGet/SetDate, RfcGet/SetTime | médio | `Date`/`Time` types nullable; opt-in `time.Time`; "00000000" não silencia | 🔴 | 7.50 PL3 |
| 35 | Strict date/time toggles | P | ❌ | ✅ | T1 | conv Go-side | baixo | `CheckDate`,`CheckTime` opções rejeitam inválidos | ❌ | n/a |
| 36 | rstrip | P/G | ✅ | ✅ | T1 | conv Go-side | baixo | trailing space removido em CHAR | ❌ | n/a |
| 37 | return_import_params | P/G | ✅ | ✅ | T1 | filtro direção | baixo | `ReturnImportParams=true` ecoa imports | ❌ | n/a |
| 38 | Struct mapping via tags | N/P/Y/S | ❌ | ✅ | T1 | reflection + cache | médio | tag `rfc:"BAPI_NAME,omitempty,direction=tables"` | ❌ | n/a |
| 39 | Custom FS for sapnwrfc.ini | N(`customFs`) | ❌ | ✅ | T2 | `RfcSetIniPath` + Go-side ini parser | médio | `IniFS` interface lê de Kubernetes ConfigMap | ❌ | n/a |
| 40 | Runtime ini reload | N | ❌ | ✅ | T2 | `RfcReloadIniFile` 🟡 | baixo | reload aplica ao próximo `OpenDest` | 🔴 | 7.50 PL3 |
| 41 | Crypto library loading | N(`loadCryptoLibrary`) | ❌ | ✅ | T1 | `RfcLoadCryptoLibrary` 🟡 | médio | path libsapcrypto carrega; SNC funciona | 🔴 | 7.50 PL10+ 🟡 |
| 42 | RfcResetServerContext | J/N/P | ❌ | ✅ | T1 | `RfcResetServerContext` ✅ | baixo | ABAP session reset entre calls | 🔴 | 7.50 PL3 |
| 43 | Language ISO/SAP | N | ❌ | ✅ | T1 | `RfcLanguageIsoToSap`,`RfcLanguageSapToIso` 🟡 | baixo | `LanguageISOToSAP("en")="E"` | ❌ | 7.50 PL3 |
| 44 | Trace control | J/N/P | ❌ | ✅ | T1 | `RfcSetTraceLevel`,`RfcSetTraceDir`,`RfcSetTraceEncoding`,`RfcSetTraceType` ✅ | baixo | `nwrfc.SetTraceLevel(2)` redireciona arquivos | 🔴 | 7.50 PL3 |
| 45 | Typed error hierarchy | P/J/N | 🔶 | ✅ | T1 | `RFC_ERROR_INFO` decode | médio | Todas as 13 categorias retornáveis | 🔴 | 7.50 PL3 |
| 46 | Library presence check | S(`EnsureLibraryPresent`) | ❌ | ✅ | T1 | `dlopen`/`LoadLibraryW` probe + `RfcGetVersion` | baixo | `EnsureSDK()` retorna erro descritivo se SDK ausente | ❌ | n/a |
| 47 | Build tag nwrfc_nosdk | — | ❌ | ✅ | T1 | nenhum (stub) | baixo | `go build -tags nwrfc_nosdk` produz binário; runtime erra `ErrSDKUnavailable` | ❌ | n/a |
| 48 | Async/non-blocking idiomática | N/P/Y | ❌ | ✅ | T1 | goroutine + ctx | baixo | múltiplos `Conn.Call` em goroutines não corrompem; `synctest` clean | 🔴 | n/a |
| 49 | Runtime/backend abstraction | Y(`SAPRfcRuntime`) | ❌ | ✅ | T1 | `Backend` interface | baixo | Mock backend roda mesma test suite | ❌ | n/a |
| 50 | Connection event listeners | J/S | ❌ | ✅ | T2 | hook em `Conn` lifecycle | baixo | `OnStateChange(fn)` chamado em open/close/broken | ❌ | n/a |
| 51 | Codegen typed BAPI clients | NENHUM | ❌ | ✅ | T4 | `Describe` Go-side | médio | `nwrfc-gen describe -fn BAPI_USER_GET_DETAIL` produz `bapi_user_get_detail.go` | 🔴 | n/a |
| 52 | OpenTelemetry/slog opt-in | NENHUM core | ❌ | ✅ | T2 | `nwrfcotel/` subpkg | baixo | spans com SAP system+function name; redaction ativa | ❌ | n/a |
| 53 | Mock backend | NENHUM | ❌ | ✅ | T4 | implementa `Backend` | médio | Test suite `nwrfc/` passa com mock injetado | ❌ | n/a |

---

## 4. Go Modernization Plan

Versão alvo: **Go 1.25** (`toolchain go1.25.0`). Justificativa:

- `testing/synctest` GA — crítico para testar Pool, Server, cancellation sem timing flakes ✅ confirmed [Go 1.25 release notes](https://go.dev/doc/go1.25).
- `slog` maduro com Handler interface estável.
- `go/types` com generics maduros, `iter.Seq` em `slices`/`maps`.
- Toolchain directive permite minar com `go 1.23` mas exigir toolchain `1.25`.

### 4.1 `context.Context` — política

**Regra:** primeiro parâmetro em qualquer função que possa bloquear, abrir socket, alocar handle SDK, ou rodar mais de 1ms.

- `Open`, `Ping`, `Call`, `Describe`, `Submit/Confirm` (tx, unit), `Acquire` (pool) → `ctx`.
- `Close`, `Release` → sem `ctx` (idempotente, deve ser não-bloqueante e finalizar mesmo em shutdown).
- `Conn.Cancel(ctx)` interno: dispara `RfcCancel(handle)` 🟡 quando `ctx.Done()` fecha durante `Invoke`. Implementação: goroutine watcher por chamada que faz `select { case <-ctx.Done(): backend.Cancel(handle); case <-done: }`.
- `Open(ctx, ...)`: honra `ctx.Err()` antes de entrar no backend/SDK. Durante `RfcOpenConnection`, gorfc não consegue chamar `RfcCancel`, porque o `RFC_CONNECTION_HANDLE` só existe depois do retorno bem-sucedido da abertura. Timeout de conexão em voo deve ser configurado no SAP NWRFC SDK / gateway / SAProuter / rede do sistema.

**Não fazer:** propagar `ctx` em `Close()` para honrar timeout — `Close` deve ser síncrono e rápido; se SDK travar, ele trava. Documentar.

### 4.2 Goroutines

- **Pool:** workers reais sob `sync.Mutex`/`sync.Cond` ou `chan *Conn` semaphore.
- **Server:** uma goroutine por requisição ABAP recebida; pool de workers configurável.
- **Cancel watcher por call:** spawn goroutine para `select` ctx vs done. Encerra em microssegundos via channel close.
- **Não criar `async/await` falso.** Cada operação bloqueia a goroutine que a chamou. Usuário multiplexa criando goroutines.

### 4.3 `errgroup` e `chan`

- `errgroup.Group` em Server para parar handlers em shutdown ordenado.
- `chan` em Pool para sinalizar disponibilidade.
- `context.AfterFunc` (Go 1.21+) considerado para cancel-watcher; **prefere `select`** porque AfterFunc não permite cancelamento explícito sem `Stop()` race com a função executando.

### 4.4 Erros: `errors.Is/As`, wrapping, `errors.Join`

- Toda função de SDK retorna erro tipado; `*RfcSDKError` implementa `Is(target error) bool` permitindo `errors.Is(err, nwrfc.ErrLogon)`.
- `Pool.Close` agrega erros via `errors.Join` ao fechar todas as conns.
- `Server.Stop` agrega via `errors.Join` ao parar handlers, threads, e fechar registration.
- `Conn.Close` em estado `broken` retorna `errors.Join(closeErr, originalBrokeErr)`.
- Erros wrappam preservando original: `fmt.Errorf("Call %q: %w", name, err)`.

### 4.5 `slog` com redaction

- `nwrfcotel/slog.go` define `RedactHandler` que envolve qualquer `slog.Handler` e remove fields cujo nome bate em allowlist:

  ```
  redactedKeys := []string{
      "passwd", "password", "mysapsso2", "x509cert",
      "snc_myname", "snc_partnername", "tls_client_pse",
      "bearer", "saml2",
  }
  ```

- Valores são substituídos por `"<redacted:N bytes>"`.
- Aplicado também a `*RfcSDKError.AbapMsgVx` quando categoria é `LogonError` (mensagens podem ter senha em claro).
- Handler é opt-in. Lib core nunca instancia logger global. Recebe via `Conn.WithLogger(*slog.Logger)`.

### 4.6 Generics

**Onde usa:**

- `nwrfc.Call[T any](ctx, conn, name, in, *T) (CallResult, error)` — output struct genérica.
- `Decode[T any](descr FunctionDescription, raw map[string]any) (T, error)` — utilitário codegen-friendly.
- `BAPIRet2.Filter(by Severity)` — sem generic, mas `BAPIRet2{}.Each(yield func(BAPIRet2) bool)` (iter.Seq).

**Onde NÃO usa:**

- `Conn`, `Pool`, `Session` — não há ganho; tipos concretos.
- Erros — usar interfaces e type assertions.

### 4.7 Struct tags

```
type In struct {
    Username string    `rfc:"USERNAME"`
    Password string    `rfc:"PASSWORD,redact"`            // redaction hint
    Date     nwrfc.Date `rfc:"DATE_FROM,omitempty"`
    Lines    []Line    `rfc:"LINES,direction=tables"`
    Internal string    `rfc:"-"`                          // skip
}
```

Tags suportadas:

- name (positional, default = upper-snake do field name).
- `omitempty` — não envia se zero-value.
- `notrequested` — equivalente a `RfcSetParameterActive(false)` 🟡.
- `direction=import|export|changing|tables|return` — só relevante quando ABAP é ambíguo (raro).
- `redact` — redação automática em logs/spans.
- `-` — skip.

### 4.8 Decimais — estratégia

Política: **`string` por default; `Decimal` interface para opt-in.**

```
type Decimal interface {
    Sign() int
    String() string
    UnmarshalABAPDecimal(s string) error
    MarshalABAPDecimal() (string, error)
}
```

Implementações fornecidas:

- `nwrfc.StringDecimal` — type alias `string`. Default.
- Adapters em subpacotes opt-in:
  - `nwrfcdec/apd/` para [cockroachdb/apd](https://github.com/cockroachdb/apd) (IEEE 754 decimal, mais correto para DECF34).
  - `nwrfcdec/shopspring/` para [shopspring/decimal](https://github.com/shopspring/decimal).

Trade-off:

- `apd.Decimal` mapeia melhor em IEEE 754r (ABAP DECF16/DECF34). Recomendado.
- `shopspring/decimal` é mais comum em Go. Aceita.
- `string` é o mais seguro: nunca perde precisão; transforma em outra coisa só na fronteira do app.

### 4.9 Date/Time — estratégia

```
type Date struct {
    Year, Month, Day int
    Zero             bool         // ABAP "00000000"
}
type Time struct {
    Hour, Minute, Second int
    Zero                 bool     // "000000"
}
type UTCLong struct {
    time.Time            // UTC, sub-microsec precision
    Zero  bool
}
```

- ABAP "00000000" → `Date{Zero:true}` — **explícito, não silencioso** (corrige bug atual em `gorfc/gorfc.go:833-835`).
- Opcional `Date.AsTime() (time.Time, error)` retorna erro se `Zero`.
- Validators em `internal/timeext/`:
  - `ValidateDATS(s string) error` — exatamente 8 dígitos, ano 0001..9999, mes/dia válidos.
  - `ValidateTIMS(s string) error` — exatamente 6 dígitos, hh<24, mm/ss <60.
  - `ValidateUTCLONG(s string) error` — formato `YYYY-MM-DDTHH:MM:SS.fffffff`.
- Toggles em `CallOptions`:
  - `CheckDate bool` (default true) — rejeita inválido.
  - `CheckTime bool` (default true).
  - `AllowZeroDate bool` (default false) — quando true, aceita `00000000`; quando false, ABAP "00000000" → `nwrfc.ErrZeroDate`. **Decisão:** default false porque AGENTS.md proíbe silent fallback. Documentar quebra contra gorfc upstream.

### 4.10 `iter` e `slices`/`maps`

- `iter.Seq[BAPIRet2]` em `BAPIRet2.All()` para iteração lazy.
- `slices.SortFunc` para ordenar mensagens BAPIRET2 por severidade.
- `maps.Clone` em `Params` para evitar partilha de mapa cliente.

### 4.11 Fuzz

- `internal/ucs2.FuzzRoundtrip(f *testing.F)`: fuzz `UTF8↔UTF16` cobrindo BMP, surrogate pairs, emoji, CJK 4-byte.
- `internal/bcd.FuzzParse`: BCD malformados não devem panicar.
- `nwrfc.FuzzTagParse`: tags malformadas no struct não panicam.

### 4.12 Race detector

- CI obrigatório `go test -race ./...`.
- Pool e Server stress-tested com `-count=100`.

### 4.13 govulncheck

- Step CI: `govulncheck ./...`. Falha em vuln high/critical.
- Run em `nwrfcotel/`, `nwrfcparam/`, `nwrfcmock/`, `cmd/`. **Não em `internal/sdkbackend/`** — `govulncheck` analisa puro Go; CGO não é coberto.

### 4.14 PGO

- Não há PGO em T1. Marcar como possível em T2 se benchmark Pool/Server mostrar hot path estável.

### 4.15 Build constraints

```
// internal/sdkbackend/conn.go
//go:build cgo && !nwrfc_nosdk

// internal/nosdkbackend/stub.go
//go:build !cgo || nwrfc_nosdk
```

Plataformas suportadas declaradas:

- `linux/amd64`, `linux/arm64` ✅ (NWRFC SDK suporta).
- `windows/amd64` ✅ (com toolchain MinGW-w64 ou `zig cc`).
- `darwin/amd64`, `darwin/arm64` 🔶 best-effort (tier-2, sem CI dedicado).

### 4.16 Toolchain directive

```
module github.com/cjordaoc/gorfc

go 1.23

toolchain go1.25.0
```

Compatibilidade: a interface pública é Go 1.23-compatível (compila); o toolchain 1.25 é usado para build/test moderno. Quando 1.26 sair em ago/2026, atualizar com cautela — sem quebrar `go 1.23`.

### 4.17 Workspace

- Único módulo no MVP. Adicionar workspace só quando `cmd/nwrfc-gen` evoluir para módulo separado em T4.

### 4.18 API e Go 1 promise

- `nwrfc/` versão `v0.x.x` durante T1+T2; promove a `v1.0.0` após congelamento de assinaturas.
- Após `v1.0.0`: nenhuma quebra de API; novas features via novos métodos/types.
- Mudanças quebradoras → `v2/` em path.

---

## 5. Public API Proposal

> Esboços para guiar implementação. Não são arquivos finais.

### 5.1 Conexão direta

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
```

### 5.2 Conexão por destination

```go
conn, err := nwrfc.OpenDest(ctx, "PRD",
    nwrfc.WithIniPath("/etc/nwrfc"),
    nwrfc.WithCryptoLib("/usr/local/sap/sapcrypto/libsapcrypto.so"),
)
```

### 5.3 Pool

```go
pool, err := nwrfc.NewPool(nwrfc.PoolConfig{
    Params:         params,
    Min:            2,
    Max:            16,
    AcquireTimeout: 5*time.Second,
    IdleTimeout:    10*time.Minute,
    HealthCheck:    30*time.Second,
    MaxLifetime:    1*time.Hour,
    OnError:        func(err error) { logger.Warn("pool", "err", err) },
})
if err != nil { return err }
defer pool.Close(context.Background())

c, err := pool.Acquire(ctx)
if err != nil { return err }
defer pool.Release(c)
```

### 5.4 Call simples (dinâmico)

```go
res, err := conn.CallMap(ctx, "STFC_CONNECTION", map[string]any{
    "REQUTEXT": "hi",
})
fmt.Println(res.Map["ECHOTEXT"])
```

### 5.5 Call tipada com tags

```go
type StfcIn  struct { ReqText string `rfc:"REQUTEXT"` }
type StfcOut struct {
    EchoText string `rfc:"ECHOTEXT"`
    RespText string `rfc:"RESPTEXT"`
}

var out StfcOut
_, err := nwrfc.Call(ctx, conn, "STFC_CONNECTION", StfcIn{ReqText: "hi"}, &out)
```

### 5.6 Metadata describe

```go
fd, err := conn.Describe(ctx, "BAPI_USER_GET_DETAIL")
for _, p := range fd.Parameters {
    fmt.Println(p.Name, p.Direction, p.Type, p.Optional, p.Decimals)
}
```

### 5.7 Stateful session

```go
sess, err := conn.Session(ctx)
if err != nil { return err }
defer sess.Close()

if _, err := sess.Call(ctx, "BAPI_PO_CREATE1", inHdr, &outHdr); err != nil {
    _ = sess.Rollback(ctx)
    return err
}
return sess.Commit(ctx)
```

### 5.8 Context timeout/cancel

```go
ctx, cancel := context.WithTimeout(parent, 30*time.Second)
defer cancel()

_, err := conn.Call(ctx, "Z_LONG_REPORT", in, &out)
if errors.Is(err, nwrfc.ErrCancelled) { /* ctx ended */ }
if errors.Is(err, nwrfc.ErrTimeout)   { /* deadline */ }
```

`Open(ctx, params)` has a narrower cancellation contract than `Call`:
the context is checked before `RfcOpenConnection`, but there is no
`RFC_CONNECTION_HANDLE` available while the SDK is opening the
connection, so `RfcCancel` cannot be used for in-flight open
cancellation. Open-timeout behavior must be configured through
destination/SAP network settings such as SAP gateway CPIC connection
timeouts, SAProuter, or OS network limits.

T3 validation record (2026-05-14): the local workspace did not have
`sapnwrfc.h`, `libsapnwrfc`, `sapnwrfc.dll`, a SDK ZIP, or the
`GORFC_TEST_*` variables needed to open a SAP-backed test connection, and
the documented example dispatcher endpoint was not reachable from this
host. Therefore no SDK-level `RfcOpenConnection` timeout key is accepted
as verified in this plan. The concrete SAP-owned setting to document for
CPIC setup is the gateway profile parameter `gw/cpic_timeout`. If a
deployment needs a stronger wall-clock bound than the SAP landscape
enforces, the accepted workaround is an external supervisor/orchestrator
timeout that can terminate the process; gorfc must not fake cancellation
by leaving an in-flight native open call running without an owned handle.

### 5.9 notRequested

```go
res, err := conn.CallMap(ctx, "BAPI_X", in,
    nwrfc.WithNotRequested("HUGE_TABLE_OUT", "ANOTHER_OUT"),
)
```

### 5.10 BAPIRET2 helper

```go
import "github.com/cjordaoc/gorfc/nwrfcparam"

type Out struct {
    Address ADDRESS3              `rfc:"ADDRESS"`
    Return  []nwrfcparam.BAPIRet2 `rfc:"RETURN"`
}

var out Out
_, err := nwrfc.Call(ctx, conn, "BAPI_USER_GET_DETAIL", in, &out)
if err != nil { return err }

if abapErr := nwrfcparam.AsError(out.Return); abapErr != nil {
    return abapErr   // implements error + RFCError
}
```

### 5.11 tRFC

```go
tx, err := conn.BeginTransaction(ctx)
if err != nil { return err }
defer tx.Destroy()

if err := tx.Call(ctx, "BAPI_X", inA); err != nil { return err }
if err := tx.Call(ctx, "BAPI_Y", inB); err != nil { return err }
if err := tx.Submit(ctx); err != nil { return err }

return tx.Confirm(ctx)
```

### 5.12 qRFC

```go
tx, err := conn.BeginTransaction(ctx, nwrfc.WithQueueName("CUSTOMER_001"))
// resto igual ao tRFC
```

### 5.13 bgRFC

```go
unit, err := conn.BeginUnit(ctx, nwrfc.UnitOptions{
    Type:        nwrfc.UnitQueued,         // T or Q
    QueueNames:  []string{"CUSTOMER_42"},
    Attributes:  nwrfc.UnitAttributes{...},
})
if err != nil { return err }

if err := unit.Call(ctx, "BAPI_X", inA); err != nil { return err }
if err := unit.Call(ctx, "BAPI_Y", inB); err != nil { return err }
if err := unit.Submit(ctx); err != nil { return err }
return unit.Confirm(ctx)
```

### 5.14 Server handler

```go
srv, err := nwrfc.NewServer(ctx, nwrfc.ServerConfig{
    GwHost:     "sap.example.com",
    GwService:  "sapgw00",
    ProgramID:  "GO_SERVER_1",
    RepoConn:   conn,                       // for metadata fetch
    OnError:    func(e error) { logger.Warn("srv", "err", e) },
})
if err != nil { return err }

srv.Register("Z_HELLO", func(ctx context.Context, req nwrfc.Request) error {
    var in struct { Name string `rfc:"I_NAME"` }
    if err := req.Bind(&in); err != nil { return err }
    return req.Reply(map[string]any{"E_GREETING": "Hello, " + in.Name})
})

srv.RegisterUnit(nwrfc.UnitHandlers{
    OnCheck:   func(ctx, uid) error { ... },
    OnCommit:  func(ctx, uid) error { ... },
    OnRollback: func(ctx, uid) error { ... },
    OnConfirm:  func(ctx, uid) error { ... },
    OnGetState: func(ctx, uid) (nwrfc.UnitState, error) { ... },
})

return srv.Start(ctx)        // blocks until ctx done; clean shutdown
```

### 5.15 Throughput metrics

```go
tp, err := nwrfc.NewThroughput()
if err != nil { return err }
defer tp.Close()

if err := tp.Attach(conn); err != nil { return err }

// ... chamadas ...

stats := tp.Read()
fmt.Println(stats.Calls, stats.SentBytes, stats.ApplicationTime)
```

### 5.16 OpenTelemetry opt-in

```go
import "github.com/cjordaoc/gorfc/nwrfcotel"

tp := otel.GetTracerProvider()
mp := otel.GetMeterProvider()

conn, err := nwrfc.Open(ctx, params,
    nwrfc.WithObserver(nwrfcotel.New(tp, mp)),
)
// every Call emits span "rfc.call BAPI_X"; throughput goes to OTel meter
```

### 5.17 Mock backend

```go
import (
    "github.com/cjordaoc/gorfc/nwrfc"
    "github.com/cjordaoc/gorfc/nwrfcmock"
)

func TestUserService(t *testing.T) {
    mock := nwrfcmock.New()
    mock.On("BAPI_USER_GET_DETAIL").Return(map[string]any{
        "ADDRESS": map[string]any{"FIRSTNAME": "Ada"},
        "RETURN":  []map[string]any{},
    })
    nwrfc.UseBackendForTest(t, mock)

    conn, _ := nwrfc.Open(t.Context(), nwrfc.Params{Dest: "MOCK"})
    defer conn.Close()

    var out struct {
        Address struct{ Firstname string `rfc:"FIRSTNAME"` } `rfc:"ADDRESS"`
    }
    _, err := nwrfc.Call(ctx, conn, "BAPI_USER_GET_DETAIL", nil, &out)
    require.NoError(t, err)
    require.Equal(t, "Ada", out.Address.Firstname)
}
```

### 5.18 Codegen

```bash
# T4
nwrfc-gen describe --conn DEV --fn BAPI_USER_GET_DETAIL --out internal/sap/users.go
```

Produz:

```go
// Code generated by nwrfc-gen DO NOT EDIT.
package sap

type BAPIUserGetDetailIn struct {
    Username string `rfc:"USERNAME"`
    CacheRes string `rfc:"CACHE_RESULTS,omitempty"`
}
type BAPIUserGetDetailOut struct {
    Address Address3              `rfc:"ADDRESS"`
    Return  []nwrfcparam.BAPIRet2 `rfc:"RETURN"`
}

func BAPIUserGetDetail(ctx context.Context, c *nwrfc.Conn,
    in BAPIUserGetDetailIn) (out BAPIUserGetDetailOut, err error) {
    _, err = nwrfc.Call(ctx, c, "BAPI_USER_GET_DETAIL", in, &out)
    return
}
```

---

## 6. Internal cgo Binding Strategy

### 6.1 Encapsulamento de handles

Cada handle SDK vira um struct opaco:

```
// internal/sdkbackend/handle.go
type connHandle struct {
    p   C.RFC_CONNECTION_HANDLE
    mu  sync.Mutex            // SDK não é thread-safe sobre o mesmo handle
    state atomic.Uint32       // open|broken|closed
}
```

Lifecycle:

- Construção apenas em `sdkbackend.Open(ctx, params)`.
- Destruição apenas em `Close(handle)`. **Sem `runtime.SetFinalizer`** para liberar handle. Defensivamente, **usar** finalizer apenas para emitir `slog.Warn("Conn leaked")` se GC pegar handle em estado open. Ver §6.4.
- Cada operação adquire `mu` antes de qualquer chamada SDK; libera após retorno; libera também se goroutine de cancel chamar `RfcCancel` (esta usa mutex separado).

### 6.2 Conversão SAP_UC ↔ UTF-8

Mover o que existe em `gorfc/gorfc.go:144-158` (`fillString`/`wrapString`) para `internal/sdkbackend/ucs2.go`. Corrigir bugs:

1. Em `fillString`, se `RfcUTF8ToSAPUC` falha, fazer `C.free(unsafe.Pointer(sapuc))` antes de retornar — fix do leak.
2. Eliminar os dois `defer C.free(unsafe.Pointer(cValue))` em `gorfc/gorfc.go:187-188`; usar variável que captura o ponteiro **após** o switch e liberar em defer único:

   ```
   var allocated *C.SAP_UC
   defer func() { if allocated != nil { C.free(unsafe.Pointer(allocated)) } }()
   // switch atribui allocated ao alocar
   ```

3. Padronizar liberação: pareamento `mallocU` → `free` é OK na prática mas 🟡 **requires SDK verification** contra o programming guide; alternativa segura é `RfcSAPUCToUTF8` direto onde possível.

### 6.3 RFC_ERROR_INFO extraction

Centralizado em `internal/sdkbackend/errors.go`:

```
func wrapInfo(info *C.RFC_ERROR_INFO) RfcSDKError {
    return RfcSDKError{
        Code:    int(info.code),
        Group:   uint32(info.group),
        Key:     ucs2.Wrap(&info.key[0]),
        Message: ucs2.Wrap(&info.message[0]),
        AbapMsgClass:  ucs2.Wrap(&info.abapMsgClass[0]),
        AbapMsgType:   ucs2.Wrap(&info.abapMsgType[0]),
        AbapMsgNumber: ucs2.Wrap(&info.abapMsgNumber[0]),
        AbapMsgV1: ucs2.Wrap(&info.abapMsgV1[0]),
        AbapMsgV2: ucs2.Wrap(&info.abapMsgV2[0]),
        AbapMsgV3: ucs2.Wrap(&info.abapMsgV3[0]),
        AbapMsgV4: ucs2.Wrap(&info.abapMsgV4[0]),
    }
}
```

E `func toError(rc C.RFC_RC, info *C.RFC_ERROR_INFO) error` aplica taxonomia da §7 mapeando `info.group` para tipo concreto.

### 6.4 Memory safety

Regras:

- Nenhuma alocação C cross-goroutine sem ownership claro.
- `C.CBytes`, `C.CString`, `C.malloc`/`mallocU` sempre pareados com `defer C.free` ou liberação explícita após o uso bem-sucedido.
- `unsafe.Pointer` apenas para conversões diretas Go↔C; nunca persistido em struct Go.
- Após `RfcInvoke`, ponteiros retornados (e.g. `RFC_FUNCTION_HANDLE`) são válidos só até `RfcDestroyFunction`. `defer destroy` imediatamente após `Create`.
- 🟡 **requires SDK verification**: `RfcCreateFunction` retorna handle alocado pelo SDK; precisa `RfcDestroyFunction`. `RfcGetFunctionDesc` retorna handle **cacheado pelo SDK**; **não** chamar destroy. Confirmar com programming guide.
- `runtime.LockOSThread` → não é necessário hoje 🔵 (NWRFC SDK não vincula handle a thread); 🟡 verificar especificamente para Server (alguns SDKs C exigem que o thread que `RfcRegisterServer` chamou também faça `RfcStartOrResumeServer`).

### 6.5 Thread safety

NWRFC SDK é declarado thread-safe (afirmação SAP), mas:

- **Mesmo handle não pode ser usado por duas threads simultaneamente.** → `Conn.mu`.
- Handles diferentes em threads diferentes são OK. → Pool habilita concorrência.
- `RfcInit` é chamado uma vez global; protegido por `sync.Once`.
- `RfcCancel` pode ser chamado de **outra** thread enquanto `RfcInvoke` bloqueia — esse é o ponto. `Cancel` usa **mutex separado** ou atomic flag para não deadlock com `mu`. Padrão: `Conn` tem `mu` para chamadas SDK que mutam estado, e um pointer atômico para o handle que `Cancel` lê sem lock.

### 6.6 Server callbacks: ponte C → Go

Padrão estabelecido (cgo "cgo handle" pattern, Go 1.17+):

```
//export goServerFunctionTrampoline
func goServerFunctionTrampoline(handle C.uintptr_t, fnDesc C.RFC_FUNCTION_DESC_HANDLE,
    funcCont C.RFC_FUNCTION_HANDLE, errInfo *C.RFC_ERROR_INFO) C.RFC_RC {

    h := cgo.Handle(handle).Value().(*serverState)
    // dispatch para handler Go registrado
    ...
}
```

Riscos e mitigações:

- **Goroutine criada de C:** chamar Go a partir de C exige que o callback rode num thread Go ou Go-managed. Cgo gerencia isso, mas pode mover entre threads. Resolver `runtime.LockOSThread` no callback handler 🟡 verify.
- **Panic Go atravessar para C:** sempre `defer func() { if r := recover(); r != nil { ... }}` no trampoline, retornar `RFC_EXTERNAL_FAILURE`.
- **Bloqueio no handler:** ABAP fica esperando reply; aplicar `ctx` derivado de `srv.ctx` com timeout configurável.
- **`cgo.Handle` para passar ponteiros Go:** seguro; alternativa é registry global, **evitar**.

### 6.7 Build tags / fallback

```
// internal/sdkbackend/bridge.go
//go:build cgo && !nwrfc_nosdk
package sdkbackend

// internal/nosdkbackend/stub.go
//go:build !cgo || nwrfc_nosdk
package nosdkbackend
```

`init()` em cada arquivo de backend chama `backend.Register(name, impl)`. Apenas um deles compila por build → apenas um se registra → backend.Default() retorna o ativo.

### 6.8 Detecção de versão / capabilities

```
func (b *sdkBackend) Capabilities() (Capabilities, error) {
    v := b.LibraryVersion()
    return Capabilities{
        WebSocketRFC:        v.AtLeast(7,50,10),    // 🟡 verify minimum PL
        Throughput:          v.AtLeast(7,53,0),     // 🟡 verify
        BgRFC:               v.AtLeast(7,50,5),     // 🟡 verify
        UTCLong:             v.AtLeast(7,50,0),
        FastSerialization:   v.AtLeast(7,50,11),    // 🟡 verify
    }, nil
}
```

`Conn.Use(featureX)` retorna `nwrfc.ErrUnsupportedFeature` quando capability falsa.

`RfcGetVersion(major, minor, patchlevel)` ✅ confirmado disponível em qualquer SDK 7.50.

---

## 7. Error Taxonomy

Inspirada em PyRFC + JCo + node-rfc, traduzida para Go idiomático.

### 7.1 Hierarquia

```
nwrfc.RFCError                           ← interface raiz
├── nwrfc.SDKError                       ← concrete; wraps RFC_ERROR_INFO
│   ├── matched via group/code:
│   ├── nwrfc.LogonError                 ← group LOGON_FAILURE
│   ├── nwrfc.CommunicationError         ← group COMMUNICATION_FAILURE
│   ├── nwrfc.ABAPApplicationError       ← group ABAP_APPLICATION_FAILURE
│   ├── nwrfc.ABAPRuntimeError           ← group ABAP_RUNTIME_FAILURE
│   ├── nwrfc.ABAPClassicException       ← group ABAP_EXCEPTION (system exception)
│   ├── nwrfc.ABAPClassException         ← group ABAP_EXCEPTION + class-based (≥7.20)
│   ├── nwrfc.ExternalAuthorizationError ← group EXTERNAL_AUTHORIZATION_FAILURE
│   ├── nwrfc.ExternalApplicationError   ← group EXTERNAL_APPLICATION_FAILURE
│   └── nwrfc.ExternalRuntimeError       ← group EXTERNAL_RUNTIME_FAILURE
├── nwrfc.ConnError                      ← Go-side wrapping
│   ├── nwrfc.BrokenConnectionError      ← detectado em uso após erro
│   ├── nwrfc.TimeoutError               ← ctx deadline
│   └── nwrfc.CancelledError             ← ctx cancelled
├── nwrfc.MarshalError                   ← Go↔ABAP conversion
│   └── nwrfc.UnknownTypeError
├── nwrfc.ConfigError                    ← params inválidos / SDK setup
├── nwrfc.SDKUnavailableError            ← build sem SDK ou lib não encontrada
└── nwrfc.UnsupportedFeatureError        ← capability não disponível na PL
```

### 7.2 Por erro: quando, fields, redaction

| Erro | Quando | Fields públicos | Redaction |
|---|---|---|---|
| `LogonError` | Logon falhou (senha errada, user bloqueado) | `Code`, `Key="LOGON_FAILED"`, `Message`, `User`, `Client`, `SysID` | `Message` redacted se contiver `passwd=`/cookie. **`Password` nunca incluído.** |
| `CommunicationError` | Socket fechado, timeout TCP, gateway recusou | `Code`, `Key`, `Message`, `Host`, `Service` | nada |
| `ABAPApplicationError` | RFM raised exception não-classe | `Key`, `Message`, `AbapMsg{Class,Type,Number,V1..V4}`, `Function` | none |
| `ABAPRuntimeError` | DUMP ABAP (short dump) | acima + `DumpURL` opcional | none |
| `ABAPClassicException` | RAISE ... TYPE | acima | none |
| `ABAPClassException` | RAISE EXCEPTION TYPE class (≥7.20) | acima + `ClassName` | none |
| `ExternalAuthorizationError` | EXT_AUTH falhou | acima | redact ticket |
| `ExternalApplicationError` | external app raised | acima | none |
| `ExternalRuntimeError` | runtime ext (e.g. SDK lib) | acima | none |
| `BrokenConnectionError` | uso após erro ou close inesperado | `Original error` | wrap |
| `TimeoutError` | ctx deadline | `Deadline`, `FunctionName` | none |
| `CancelledError` | ctx cancelled | `FunctionName` | none |
| `MarshalError` | tipo não converte | `FieldName`, `GoType`, `ABAPType` | none |
| `UnknownTypeError` | RFCTYPE_xxx não suportado | `RfcType` | none |
| `ConfigError` | params faltando ou inválidos | `Field` | redact se param sensível |
| `SDKUnavailableError` | build nosdk ou lib not found | `Reason`, `LookupPath` | none |
| `UnsupportedFeatureError` | capability false | `Feature`, `RequiredVersion`, `CurrentVersion` | none |

### 7.3 `errors.Is/As`

```
type RFCError interface {
    error
    Category() Category
}

// Sentinel para errors.Is
var (
    ErrLogon            = sentinelError{cat: CatLogon}
    ErrCommunication    = sentinelError{cat: CatCommunication}
    ErrABAPApplication  = sentinelError{cat: CatABAPApp}
    ErrABAPRuntime      = sentinelError{cat: CatABAPRuntime}
    ErrTimeout          = sentinelError{cat: CatTimeout}
    ErrCancelled        = sentinelError{cat: CatCancelled}
    ErrSDKUnavailable   = sentinelError{cat: CatSDKUnavailable}
    ErrUnsupported      = sentinelError{cat: CatUnsupported}
    ErrBrokenConn       = sentinelError{cat: CatBrokenConn}
    ...
)

func (e *LogonError) Is(target error) bool {
    return target == ErrLogon
}
func (e *LogonError) As(target any) bool {
    if t, ok := target.(**LogonError); ok { *t = e; return true }
    return false
}
```

Uso:

```
if errors.Is(err, nwrfc.ErrLogon) { ... }
var abap *nwrfc.ABAPApplicationError
if errors.As(err, &abap) { fmt.Println(abap.AbapMsgClass) }
```

### 7.4 Redaction integrada

Cada erro implementa `LogValue() slog.Value` retornando representação redacted. `nwrfcotel.RedactHandler` propaga isso em spans.

---

## 8. Security Model

### 8.1 Credentials

- **Nunca** persistir senha em struct após uso para abrir conexão. Após `Open`, `params.Passwd` é zeroed (defensivo) ou marcado como `[]byte` que pode ser explicitamente zerado pelo caller.
- Loggers nunca recebem `Params` direto; recebem `Params.SafeString()` que omite os campos `Passwd`, `MysapSSO2`, `X509cert`, `Bearer`, `SAML2`.
- `Conn.GetConnectionAttributes()` não inclui senha.

### 8.2 sapnwrfc.ini

- Default lookup honra `RFC_INI` env, depois cwd.
- `IniFS` interface permite carregar de Kubernetes ConfigMap/Secret, S3, Vault.
- Parser Go-side em `nwrfc/ini.go` valida sintaxe e detecta linhas com `PASSWD=` em arquivos cujo modo unix permita read by group/world — emite `slog.Warn("ini-perm-too-permissive", "path", p, "mode", mode)`. Não bloqueia (pode ser intencional em containers).
- `CONTRIBUTING.md` proíbe commit de qualquer ini.
- `.gitignore` adiciona `**/sapnwrfc.ini`, `**/*.pse`, `**/sec/`.
- CI hook (pre-receive ou GHA) escaneia diff por padrões: `PASSWD=`, `MYSAPSSO2=`, `BEGIN CERTIFICATE`, `BEGIN PRIVATE KEY`, `*.wdf.sap.corp`, `*.sap.corp`.

### 8.3 Environment variables

Recomendados:

- `SAP_USER`, `SAP_PASS` (run-time only, nunca commit em manifesto público).
- `SAPNWRFC_HOME`, `RFC_INI`, `LD_LIBRARY_PATH`.
- 12-factor friendly via `nwrfc.ParamsFromEnv("SAP_")`.

### 8.4 SNC

- `SNC_LIB`, `SNC_QOP`, `SNC_MYNAME`, `SNC_PARTNERNAME` passados para SDK.
- `nwrfc.SetCryptoLib(path)` chama `RfcLoadCryptoLibrary` 🟡.
- `Params.SafeString()` redacta `SNC_MYNAME` e `SNC_PARTNERNAME` (são DNs que vazam topologia interna).

### 8.5 X509

- `Params.X509Cert` é base64 do cert; logged como `<x509:N bytes, fp=sha256:...>`.
- PEM/DER detection automática.

### 8.6 MYSAPSSO2

- `Params.MYSAPSSO2` é bytes; nunca logged.

### 8.7 SAML/Bearer

- `Params.SAML2`, `Params.Bearer` redacted.
- 🟡 **requires SDK verification** que params são suportados por essa versão.

### 8.8 Trace SDK

- `RfcSetTraceLevel` máximo 0 em produção por default.
- Trace files em `RfcSetTraceDir` podem conter dumps de payload com PII. Documentar:
  - Diretório precisa permissão restrita (0700).
  - Logrotate obrigatório.
  - Nunca habilitar > 1 em prod sem aprovação de SecOps.

### 8.9 Logs

- `slog` com `RedactHandler` por default em factory `nwrfc.NewLogger(out)`.
- Usuário pode passar próprio `*slog.Logger`; **lib documenta a expectativa de redação** mas não força.

### 8.10 OpenTelemetry spans

- Atributos default em span `rfc.call`:
  - `rfc.function_name` ✅
  - `rfc.system_id`, `rfc.client`, `rfc.user` (NÃO `passwd`) ✅
  - `rfc.bytes_sent`, `rfc.bytes_received` ✅
  - `rfc.duration_ms` ✅
  - `error.type`, `error.message` (redacted)
- **Nunca** atributos com payload do call. Default `OTEL_LOG_PAYLOAD=false`.

### 8.11 Panic recovery

- Server handlers: cada invocation tem `defer recover` que converte panic em `ABAPApplicationError` retornado para ABAP.
- Pool/Conn: panic em internal não é catched; falha rápida.

### 8.12 Examples e docs

- Toda example usa `os.Getenv` para credentials.
- Não há ASHOST hardcoded.
- `examples/` contém `Makefile` com `make run` que checa env vars e falha amigavelmente se ausentes.

### 8.13 CI

- GitHub Actions com `secrets.SAP_*` mapeados como env. **Nunca** ecoar.
- Workflow `nightly-integration.yml` requer `protected` env do GitHub; só PRs de mantenedores trigger.
- Public CI roda apenas tests SDK-free.

### 8.14 Sample configs

- `examples/configs/sapnwrfc.ini.example` com placeholders `<USER>`, `<HOST>`.
- Documentação explica lookup order, modo restrito.

### 8.15 Secret scanning

- `gitleaks` ou `trufflehog` em pre-commit hook + CI.
- Custom rules para padrões SAP: `MYSAPSSO2`, `PASSWD\s*=`, `BEGIN CERTIFICATE`, `*.sap.corp`.

### 8.16 Redaction rules — tabela objetiva

| Campo | Onde aparece | Redaction |
|---|---|---|
| `PASSWD` | Params, ini, env | `<redacted:N>` |
| `MYSAPSSO2` | Params | `<redacted:N bytes>` |
| `X509CERT` | Params | `<x509: fp=sha256:abcd...>` |
| `BEARER`, `SAML2` | Params | `<redacted:N>` |
| `SNC_MYNAME`, `SNC_PARTNERNAME` | Params | partial: `<DN:CN=***>` |
| `TLS_CLIENT_PSE` | Params | `<path:redacted>` |
| `AbapMsgVx` em LogonError | Erros | `<redacted-on-logon>` |
| Payload do call | Spans | omitido por default |
| `Param.Passwd` em Stringer | Debug | `<redacted>` |

---

## 9. Testing and Conformance Strategy

### 9.1 SDK-free unit tests (PR gate, sempre rodam em qualquer CI)

Localizadas em pacotes que não importam `internal/sdkbackend`:

| Suite | Cobertura |
|---|---|
| `nwrfc/...` exceto integração | API surface; mock backend |
| `internal/ucs2` | UTF-8↔UTF-16 round-trip; surrogate pairs; emoji; CJK 4-byte; truncamento |
| `internal/bcd` | parse BCD válido; rejeita inválido; max precisão |
| `internal/timeext` | DATS valid/invalid; TIMS; UTCLONG; "00000000"; bissexto |
| `internal/secrets` | redaction de Params, ABAP errors |
| `nwrfc/tags` | parse de struct tags; cache; reflexão |
| `nwrfcparam` | BAPIRet2 classification; empty; multi-message |
| Fuzz | `FuzzUTF8Roundtrip`, `FuzzBCDParse`, `FuzzTagParse`, `FuzzDATSValidate` |
| `synctest` | Pool acquire/release sob carga sintética |

Critério: 90%+ coverage em `nwrfc/`, 100% em `internal/secrets`, `internal/timeext`, `internal/ucs2`.

### 9.2 SDK-present local tests (rodam quando `SAPNWRFC_HOME` definido; gated build tag `nwrfc_sdk`)

- `EnsureLibraryPresent` localiza `libsapnwrfc.so`/`.dll` em `SAPNWRFC_HOME/lib`, retorna versão.
- `RfcGetVersion` retorna major/minor/patch coerente.
- Capabilities probe: `WebSocketRFC`, `Throughput`, `BgRFC` flags coerentes com versão detectada.
- Init/shutdown ciclos não vazam (valgrind opcional manual).
- Nenhum SAP backend necessário aqui — apenas SDK presente.

### 9.3 SAP integration tests (`-tags=nwrfc_integration`, env `RFC_INTEGRATION=1`)

| Test | Function | Critério | Tier introduzido |
|---|---|---|---|
| `TestPing` | `RfcPing` | 200ms < latência < 5s | T1 |
| `TestStfcConnection` | `STFC_CONNECTION` | Echo bate; UTF-8 ABAP↔Go | T1 |
| `TestStfcStructure` | `STFC_STRUCTURE` | Todos os tipos ABAP round-trip | T1 |
| `TestStfcDeepTable` | `STFC_DEEP_TABLE` | Tabela aninhada | T1 |
| `TestMetadata` | `BAPI_USER_GET_DETAIL` describe | FunctionDescription completa, parameters direção, optional | T1 |
| `TestBapiUserGetDetail` | call real | `RETURN` com BAPIRet2 não vazio se user inválido | T1 |
| `TestStatefulSession` | dois calls + commit | LUW preservado | T1 |
| `TestCancel` | RFM longo + ctx.Cancel após 1s | Erro `ErrCancelled` em <2s | T1 |
| `TestTimeout` | RFM longo + ctx 1s | `ErrTimeout` | T1 |
| `TestPoolConcurrency` | 100 calls em 16 goroutines | Sem race; throughput estável | T1 |
| `TestThroughput` | 1000 calls com Throughput attached | stats > 0 em todas as métricas | T2 |
| `TestWebSocketRFC` | open via WSHOST/WSPORT | conexão TLS estabelecida | T1/T2 (capability) |
| `TestSNC` | open com SNC params | `Attributes.SncMode == "1"` | T1 |
| `TestX509` | open com x509 | logon como usuário esperado | T2 |
| `TestSSO` | open com MYSAPSSO2 | logon | T2 |
| `TestTRFCClient` | tRFC submit + confirm | unit visível em SM58 ou similar | T3 |
| `TestQRFCClient` | qRFC SetQueueName | unit em SMQ1 | T3 |
| `TestBgRFCClient` | bgRFC submit | unit em SBGRFCMON | T3 |
| `TestServerSync` | server registra `Z_HELLO`; ABAP chama | reply válido | T2/T3 |
| `TestServerBgRFC` | bgRFC server handlers | onCommit chamado | T3 |
| `TestIDoc` | parse + build MATMAS05 | round-trip | T4 |

### 9.4 CI strategy

```
.github/workflows/
├── pr.yml                    ← cada PR: SDK-free + lint + race + fuzz curto + govulncheck
├── nightly-sdk.yml           ← noturno: SDK-present (lib instalada via cache)
├── nightly-integration.yml   ← noturno: integration vs SAP sandbox (gated; manual approval)
├── windows.yml               ← weekly: build + SDK-free em windows-amd64
├── darwin.yml                ← weekly: build em macos-arm64 (best-effort)
└── release.yml               ← em tag: build artifacts; sign; SBOM
```

**PR (público, ~5min):**

- `gofmt -l . && [ -z "$(gofmt -l .)" ]`
- `golangci-lint run` (config v2 com `default: standard`)
- `go test -race -tags=nwrfc_nosdk ./...`
- `go test -fuzz=Fuzz -fuzztime=30s` em pacotes `internal/`
- `govulncheck ./...`

**Nightly SDK-present (~15min):**

- runner com `SAPNWRFC_HOME` cached como GitHub Actions artifact (manualmente uploaded por mantenedor; nunca commitado)
- `go test -tags=nwrfc_sdk ./...` (não chega no SAP)

**Nightly integration (~30min):**

- runner protegido (env GH com approval gate)
- secrets `SAP_*` mapeados
- `go test -tags=nwrfc_integration ./...`
- artifacts: trace files (sanitizados), test report

**Race detector:** sempre em PR e nightly.

**Fuzz:** PR runs 30s; weekly runs 1h por pacote, mantém corpus em GitHub Actions cache.

**Linter:** `golangci-lint` v2 com:

```
linters:
  default: standard       # = errcheck, gosimple, govet, ineffassign, staticcheck, unused
  enable:
    - gosec               # security
    - revive              # style
    - bodyclose
    - errorlint           # errors.Is/As correctness
    - exhaustive          # switch on enums
    - nilerr
    - prealloc
    - unconvert
```

**SBOM:** `syft` em release; armazenar em `.sbom/`.

---

## 10. Roadmap by Tiers

### Tier 0 — Remediação imediata (1–2 PM)

**Objetivo:** zerar violações ativas do AGENTS.md e restaurar build sanity. Sem novas features.

**Entregáveis:**

1. Remoção de `gorfc/sapnwrfc.ini`; criação de `gorfc/sapnwrfc.ini.example` placeholder.
2. Adição de `**/sapnwrfc.ini`, `**/*.pse`, `**/sec/` em `.gitignore`.
3. Correção do build tag em `gorfc/gorfc.go:1` para `((linux || darwin || windows) && cgo)`.
4. Correção dos `defer C.free` quebrados em `gorfc/gorfc.go:187-188`.
5. Adição do `pre-commit` hook (gitleaks) e GHA `secret-scan.yml`.
6. Issue tracker abrindo `[security] credentials previously committed in upstream history` para registro permanente.
7. `docs/SECURITY.md` v0.

**Pacotes afetados:** `gorfc/`, repo root.

**Testes:** nenhum novo; CI deve manter verde.

**Critérios de aceite:**

- `git grep -i "PASSWD\|wdf.sap.corp" -- gorfc/` retorna vazio.
- `go vet ./...` clean.
- Build em linux+darwin produz binário; em windows confirma falha **explícita** (não silent success).
- secret-scan workflow PASS em PR de teste.

**Riscos:** baixo. Mitigação: PR único, revisado.

**Fora de escopo:** API, novos pacotes, CGO refactor.

### Tier 1 — Cliente sólido para produção (4–6 PM)

**Objetivo:** wrapper RFC completo para chamadas síncronas, com pool, sessão stateful, contexto cancelável, segredo redacted, SDK-isolado.

**Entregáveis:**

- Pacote `nwrfc/` com API §5.1–§5.10.
- `internal/sdkbackend/` cobrindo Open, Close, Ping, Describe, Invoke, Cancel, ResetServerContext, Attributes.
- `internal/nosdkbackend/`.
- `nwrfc.Pool`, `nwrfc.Session`, `nwrfc.Conn`, `nwrfc.Params`, `nwrfc.CallOptions`.
- Tipos ABAP completos com policy (Date, Time, UTCLong, Decimal interface).
- Erros tipados (§7).
- Redaction (§8).
- Trace control programático.
- `EnsureSDK()` library presence check.
- Build tag `nwrfc_nosdk`.
- WebSocket RFC capability-detected.
- SNC com crypto lib loader.
- Tags `rfc:""`.
- Language ISO/SAP.
- `setIniPath`, `reloadIniFile`.
- Testes SDK-free 90% cov.
- Integration tests para Ping, STFC_CONNECTION, STFC_STRUCTURE, BAPI_USER_GET_DETAIL, Cancel, Timeout.
- `docs/INSTALL.md`, `docs/BUILD.md`, `docs/CONFIGURATION.md`, `docs/ERRORS.md`.
- `examples/ping/`, `examples/stfc_structure/`, `examples/pool/`, `examples/session/`.

**Critérios de aceite:**

- Cliente compila com `-tags nwrfc_nosdk` em qualquer CI.
- `STFC_STRUCTURE` round-trip cobre todos os 15 tipos ABAP listados em `doc/README.md` com igualdade exata.
- Pool sob 16 goroutines × 100 calls termina sem race; tput estável.
- `ctx.Cancel()` durante Invoke produz `ErrCancelled` em ≤ 2s.
- Logs nunca contêm PASSWD/MYSAPSSO2/x509 (test automatizado: `slog.NewJSONHandler` capturado, regex check).
- Documentação cobre Linux, Windows, macOS install.

**Riscos:** ver §3 — principais: Windows MinGW build, RfcCancel exato, mallocU/free pareamento.

**Dependências externas:**

- SAP NWRFC SDK 7.50 PL12+ instalado em CI nightly.
- Sandbox SAP minimal (NW Developer Edition Docker 🟡 verify; alternativa: parceiro SAP).

**Fora de escopo:**

- Server, tRFC, qRFC, bgRFC, IDoc, throughput, observability.

### Tier 2 — Observabilidade, providers, throughput, server síncrono (6–9 PM)

**Objetivo:** API moderna com observabilidade native, server síncrono, providers programáticos.

**Entregáveis:**

- `nwrfcotel/` opt-in (spans, metrics, otelslog bridge).
- `nwrfc.Throughput` API.
- `nwrfc.DestinationProvider`, `nwrfc.ServerProvider` interfaces.
- `nwrfc.IniFS` (custom FS).
- Server síncrono: `nwrfc.NewServer`, `srv.Register`, `srv.Start/Stop`. Apenas RFM síncronas — sem t/q/bgRFC ainda.
- `nwrfcparam.BAPIRet2` helper completo.
- Auth: SSO ticket, x509, SAML/Bearer.
- Metadata cache + invalidate.
- Connection event listeners.
- Examples: `examples/server/`, `examples/otel/`, `examples/bapi_user_get_detail/`, `examples/customfs/`.

**Critérios de aceite:**

- `STFC_CONNECTION` server (registrado por gw) recebe call de ABAP `CALL FUNCTION DEST 'GO_SERVER'` e retorna em menos de 100ms.
- OTel spans aparecem em Jaeger com atributos esperados; sem payload em default config.
- Throughput attached produz stats não-zero após 100 calls.
- Custom IniFS lê de fakefs em test.
- DestinationProvider chamado on-demand; cache cooperativo.

**Riscos:** server cgo callbacks (§6.6), metadata cache invalidation race.

**Fora de escopo:** tRFC/qRFC/bgRFC, IDoc, codegen.

### Tier 3 — Protocolos avançados e server bg/trans (8–12 PM)

**Objetivo:** paridade JCo em protocolos.

**Entregáveis:**

- tRFC client + server.
- qRFC client + server.
- bgRFC client + server (handlers onCheck/onCommit/onConfirm/onRollback/onGetState).
- `nwrfc.Repository` abstraction (cache de descritores compartilhado entre Conns).
- `nwrfc.Backend` documentado como ponto de extensão.
- `cmd/nwrfc` CLI (ping, call, describe, version).
- Integration tests reais para tRFC/qRFC/bgRFC contra SAP sandbox.

**Critérios de aceite:**

- bgRFC unit Submit + ABAP `SBGRFCMON` mostra unit em fila; após Confirm sumiu.
- tRFC server recebe unit; OnCommit invocado; ABAP `SM58` mostra status correto.
- qRFC com 3 units em mesma queue executa em ordem.
- Repo pre-loaded reduz latência da primeira call de N→1.

**Riscos:** server bg callbacks são onde PyRFC quebrou; maior cuidado em testes; possível bloqueio em validação SAP real.

**Dependências externas:** SAP sandbox com bgRFC config (SBGRFCCONF).

### Tier 4 — Diferenciação Go-native (6–12 PM)

**Objetivo:** features que nenhum wrapper estudado oferece.

**Entregáveis:**

- `cmd/nwrfc-gen` codegen de cliente BAPI tipado.
- `nwrfcmock/` mock backend.
- `nwrfcidoc/` IDoc parser/builder (sem CGO; opera sobre output de RFC IDoc).
- Bridge HTTP/gRPC opcional em `cmd/nwrfc-bridge` (expõe RFC sobre HTTP/JSON ou gRPC).
- Helpers SAP authorization (parse SU01, BAPI_USER_GET_DETAIL).
- Migration guide para users do `gorfc` upstream.
- Promoção a `v1.0.0`.

**Critérios de aceite:**

- `nwrfc-gen describe -fn BAPI_USER_GET_DETAIL` produz pacote Go que compila.
- Mock backend roda toda a suite `nwrfc/...` com SAP simulado.
- IDoc round-trip de MATMAS05 entre Go e ABAP.

**Fora de escopo:** integração com frameworks específicos; SAP Cloud SDK Go.

---

## 11. Implementation Sequence

PRs pequenos, mergeáveis isoladamente, com rollback claro.

| # | Nome | Objetivo | Pacotes | Mudanças | Testes | Aceite | Risco | Rollback |
|---|---|---|---|---|---|---|---|---|
| **T0.1** | sec: remove committed credentials | Remover ini com creds; placeholder | gorfc/, .gitignore, docs/SECURITY.md v0 | delete + example + ignore + scan workflow | secret-scan workflow | grep clean | nenhum | revert |
| **T0.2** | fix: build tag windows guard | Build tag correto | gorfc/gorfc.go:1 | tag rewrite | go vet | build linux+darwin OK; windows compila com cuidado ou requires CGO | baixo | revert |
| **T0.3** | fix: cgo memory leak in fillVariable | corrige `defer C.free` | gorfc/gorfc.go | refactor switch | leak test (criação repetida) | leak ausente em 1k iterações | médio | revert |
| **T0.4** | docs: feasibility, sdk_functions_map, feature_matrix, roadmap | docs core do plano | docs/* | escrita | linkcheck | revisores aprovam | nenhum | revert |
| **T1.1** | refactor: introduce internal/backend interface | Cria contrato | internal/backend/, internal/sdkbackend/ skeleton, internal/nosdkbackend/ | shell de Backend interface; sdkbackend só compila com cgo+nwrfc_sdk; nosdk stub | unit: registry choice | go build sem SDK funciona; com SDK funciona | médio | revert sem afetar gorfc/ legado |
| **T1.2** | feat: nwrfc package skeleton + Conn lifecycle | API surface mínima | nwrfc/ | Conn, Params, Open, Close, Ping; sem Call ainda | mock backend tests | mock acquires Conn; close idempotente | médio | revert sem afetar legado |
| **T1.3** | feat: typed errors hierarchy | §7 | nwrfc/errors.go, internal/sdkbackend/errors.go | erros + group→tipo + Is/As + redaction LogValue | unit + redaction tests | errors.Is(err, ErrLogon) funciona | baixo | revert |
| **T1.4** | feat: ucs2 + bcd + timeext util packages | tipos ABAP base | internal/ucs2/, internal/bcd/, internal/timeext/ | Date, Time, UTCLong, validators, parsers | fuzz round-trip | 100% cov, fuzz passes | baixo | revert |
| **T1.5** | feat: sdk Open/Close/Ping/Attributes | primeiros bindings reais | internal/sdkbackend/conn.go | bindings via cgo | nightly-sdk: probe lib version | RfcGetVersion retorna | médio (CGO) | revert; gorfc/ legado intacto |
| **T1.6** | feat: marshaling Go→ABAP | fill subsystem | internal/sdkbackend/fill.go | ports gorfc fill, corrige leaks | unit com mock | fillFunctionParameter compila ok | médio | revert |
| **T1.7** | feat: marshaling ABAP→Go | wrap subsystem | internal/sdkbackend/wrap.go | ports gorfc wrap, corrige bugs (zero date) | integration: STFC_STRUCTURE | round-trip exato | médio | revert |
| **T1.8** | feat: Call + CallMap + tags | API call tipada | nwrfc/call.go, nwrfc/tags.go | tag parser + reflection cache | unit + integration | Call(ctx, conn, "STFC_STRUCTURE", in, &out) ok | médio | revert |
| **T1.9** | feat: ctx cancel via RfcCancel | ctx integration | internal/sdkbackend/invoke.go | cancel watcher goroutine | integration: TestCancel | ctx.Cancel→ErrCancelled<2s | médio (RfcCancel 🟡) | feature flag desligada |
| **T1.10** | feat: Pool | concurrency | nwrfc/pool.go | Pool + PoolConfig + Stats | synctest + integration | 16x100 race-free | médio | desabilita Pool, lib ainda funciona com Conn direto |
| **T1.11** | feat: Stateful Session | LUW explícito | nwrfc/session.go | Session.Call/Commit/Rollback | integration | LUW preservado | baixo | revert |
| **T1.12** | feat: WebSocket RFC + crypto loader | transport moderno | nwrfc/crypto.go, params | params WSHOST/WSPORT/TLS_*; LoadCryptoLib | integration: TestWebSocketRFC | ws:// connect ok | médio (capability) | feature gate |
| **T1.13** | feat: trace control + ini reload + lang ISO/SAP | utilities | nwrfc/trace.go, nwrfc/ini.go, nwrfc/lang.go | thin wrappers | unit | API expostas | baixo | revert |
| **T1.14** | feat: library presence check + nosdk stub completo | graceful absence | nwrfc/version.go, internal/nosdkbackend/ | EnsureSDK + ErrSDKUnavailable | unit + nosdk build | binário sem SDK roda; erra explicitamente em runtime | baixo | revert |
| **T1.15** | docs+examples: T1 completo | docs + examples | docs/, examples/ | install, build, configuration, errors, examples | linkcheck | docs review aprovado | baixo | revert |
| **T1.16** | release: v0.1.0 | tag | tag | CHANGES + tag | SBOM | tag publicada; GoDoc verde | baixo | new tag |
| **T2.1** | feat: nwrfcparam.BAPIRet2 helper | BAPI ergonomia | nwrfcparam/ | parser + classifier + AsError | unit | tests | baixo | revert |
| **T2.2** | feat: throughput | metrics SDK | nwrfc/throughput.go, sdkbackend | bindings | integration | stats > 0 | médio (versão SDK 🟡) | feature gate |
| **T2.3** | feat: nwrfcotel subpackage | OTel + slog | nwrfcotel/ | tracer + meter + redact slog handler | unit + example | spans aparecem | baixo | subpkg opcional |
| **T2.4** | feat: DestinationProvider, ServerProvider | DI | nwrfc/provider.go | interfaces | unit | provider chamado | baixo | revert |
| **T2.5** | feat: IniFS | custom FS | nwrfc/ini.go | interface + bindings RfcSetIniPath | unit + integration | fakefs lê ini | médio | revert |
| **T2.6** | feat: metadata cache + Invalidate | controle de cache | nwrfc/metadata.go | RfcAddFunctionDesc/Remove 🟡 | integration | invalidate força refetch | médio | feature gate |
| **T2.7** | feat: Server síncrono | inbound RFC | nwrfc/server.go, sdkbackend/server.go | RfcRegisterServer + cgo trampoline | integration: TestServerSync | ABAP→Go reply | alto | feature gate; subpkg `nwrfc/server` opcional |
| **T2.8** | feat: SSO/x509/SAML auth params | auth | nwrfc/params.go, redaction | params + redaction | integration | logon ok | médio (SAML 🟡) | revert |
| **T2.9** | feat: Connection event listeners | observability | nwrfc/conn.go | hooks | unit | OnStateChange chamado | baixo | revert |
| **T2.10** | release: v0.2.0 | tag | | | SBOM | tag | baixo | new tag |
| **T3.1** | feat: tRFC client | tRFC | nwrfc/transaction.go | bindings | integration | Submit visível em SM58 | médio | feature gate |
| **T3.2** | feat: qRFC client | qRFC | nwrfc/queue.go | SetQueueName | integration | SMQ1 visível | médio | feature gate |
| **T3.3** | feat: bgRFC client | bgRFC | nwrfc/unit.go | RfcCreate/Invoke/Submit/Confirm Unit | integration: SBGRFCMON | unit visível | alto | feature gate |
| **T3.4** | feat: tRFC/qRFC/bgRFC server | server bg | nwrfc/server.go ext | handlers | integration | OnCommit invocado | alto | feature gate |
| **T3.5** | feat: Repository abstraction | metadata compartilhada | nwrfc/repository.go | shared cache | unit + bench | preload reduz latência primeira call | médio | revert |
| **T3.6** | feat: cmd/nwrfc CLI | CLI | cmd/nwrfc/ | ping, call, describe | integration | CLI roda | baixo | revert |
| **T3.7** | release: v0.3.0 | tag | | | | tag | baixo | new tag |
| **T4.1** | feat: nwrfcmock | mock | nwrfcmock/ | implements Backend | unit | suite passa com mock | médio | subpkg opcional |
| **T4.2** | feat: cmd/nwrfc-gen | codegen | cmd/nwrfc-gen/ | template + Describe + emit | unit | gen produz código compilável | médio | subpkg opcional |
| **T4.3** | feat: nwrfcidoc | IDoc | nwrfcidoc/ | parser/builder | integration | round-trip MATMAS05 | médio | subpkg opcional |
| **T4.4** | feat: HTTP/gRPC bridge | exposição | cmd/nwrfc-bridge/ | server | integration | curl→RFC | baixo | subpkg opcional |
| **T4.5** | docs: migration guide | | docs/MIGRATION_FROM_GORFC.md | escrita | linkcheck | revisores | baixo | revert |
| **T4.6** | release: v1.0.0 | tag estável | | | SBOM + signatures | tag GA | médio (compromisso de API) | next major v2/ |

---

## 12. Decision Log

| # | Decisão | Opções | Recomendação | Justificativa | Impacto | Risco |
|---|---|---|---|---|---|---|
| 1 | IDoc no core, pacote separado, ou fora do v1 | (a) core, (b) `nwrfcidoc/` separado, (c) fora | **(b)** subpacote separado em T4 | IDoc tem semântica própria (segments, control records) que não pertence ao núcleo RFC. Não bloqueia v1. JCo separa em JCoIDoc também. | Adia 6+ PM; v1 mais limpo | Usuários de IDoc esperam mais |
| 2 | WebSocket RFC obrigatório no T1 ou capability-based | (a) obrigatório, (b) capability gate | **(b)** capability gate em T1 | API expõe; runtime detecta versão SDK; erra com `ErrUnsupportedFeature` se < 7.50 PL10. Permite users com SDK antigo. | Suporte amplo; honesto sobre versão | Confusão se user não lê doc de versão |
| 3 | Server-side T1 reduzido ou T3 completo | (a) T1 sync, (b) T2 sync, (c) T3 completo, (d) só T3 completo | **(b) + (c)** T2 síncrono, T3 t/q/bg | Server síncrono é menor risco e tem demanda. Bg é separado. | Entrega incremental | Server cgo callbacks complexos |
| 4 | Codegen no T4 ou roadmap separado | (a) T4 mesmo módulo, (b) repo separado | **(a)** T4 mesmo módulo `cmd/nwrfc-gen` | Versionamento sincronizado; codegen depende de tipos `nwrfc/`. | Manutenção conjunta | Aumenta superfície |
| 5 | OTel/slog no core ou subpkg opt-in | (a) core, (b) subpkg | **(b)** subpkg `nwrfcotel/` opt-in | Lib core não deve forçar deps. Users que não querem OTel não pagam. | Core leve | Doc precisa explicar |
| 6 | Mock backend subpkg ou repo separado | (a) `nwrfcmock/` aqui, (b) repo separado | **(a)** subpkg | Versionado junto; precisa do contrato `Backend` exato. | Manutenção conjunta | Aumenta repo |
| 7 | macOS suportado ou best-effort | (a) tier-1, (b) best-effort, (c) descartar | **(b)** best-effort | SAP NWRFC SDK suporta macOS oficialmente; mas `uchar.h` é workaround. CI manual, não bloqueia release. | Comunidade Mac feliz | CI custos |
| 8 | SAP test system strategy | (a) docker dev edition, (b) parceiro, (c) cloud trial | **(c) + (a) opcional** | SAP NetWeaver Trial e ABAP Cloud Free Tier para devs (verificar 🟡); docker dev edition se acessível. | Dev local viável | Acesso é principal blocker |
| 9 | CLA / DCO | (a) DCO, (b) CLA estilo CNCF, (c) nada | **(a) DCO** | Padrão Linux Foundation, baixa fricção, registro IP claro. | Contribuições mais fáceis | Alguns enterprises preferem CLA |
| 10 | Versão mínima Go | (a) 1.23, (b) 1.24, (c) 1.25 | **(c) 1.25** mínimo `go.mod 1.23` + `toolchain 1.25` | testing/synctest GA é decisivo para testar Pool/Server. | Recursos modernos | Alguns ambientes têm Go antigo; mitigado por toolchain |
| 11 | Versão mínima NWRFC SDK | (a) 7.50 PL3, (b) 7.50 PL12, (c) 7.50 PL18 | **(b) PL12** mínimo declarado; PL18+ recomendado | PL12 cobre bgRFC server e fixes críticos; PL18 é o atual. | Compatibilidade 90%+ users | Users em PL antigos precisam upgrade |
| 12 | Política de compatibilidade semântica | (a) v0 livre, (b) v0 com warnings, (c) v1 estrito | **(b) durante T1+T2, (c) após v1.0** | v0 permite iteração; documentar quebras em CHANGES. | Iteração rápida | Users early podem reclamar |

---

## 13. Documentation Plan

| Documento | Conteúdo essencial | Tier introdução |
|---|---|---|
| `docs/PROJECT_OBJECTIVE.md` | escopo, sucesso, license boundary | já existe — atualizar T0 |
| `docs/GORFC_REVIVAL_ASSESSMENT.md` | gaps & strengths upstream | já existe — manter |
| `docs/PORTING_STRATEGY.md` | fases | atualizar T0 com tiers desta |
| `docs/PLAN.md` | este documento | T0 |
| `docs/FEASIBILITY.md` | resultado da pesquisa pure-Go vs wrapping | T0 |
| `docs/FEATURE_MATRIX.md` | matriz §3 desta resposta | T0 |
| `docs/SDK_FUNCTIONS_MAP.md` | feature → símbolos C SDK | T0; mantida sempre 🟡 verify-tracked |
| `docs/ROADMAP.md` | tiers, datas estimadas, marcos | T0 |
| `docs/ARCHITECTURE.md` | §2 desta resposta | T1.1 |
| `docs/INSTALL.md` | install SDK por SO + paths + crypto lib | T1.5 |
| `docs/BUILD.md` | flags cgo, build tags, zig cc, cross-compile | T1.5 |
| `docs/CONFIGURATION.md` | Params, ini, env, IniFS | T1.13 |
| `docs/SECURITY.md` | §8 desta resposta | T0 v0; expandido T1, T2 |
| `docs/ERRORS.md` | §7 desta resposta + exemplos `errors.Is/As` | T1.3 |
| `docs/TESTING.md` | §9 + como rodar SDK-free / SDK-present / integration | T1.7 |
| `docs/BENCHMARKS.md` | baselines SDK-free para comparar otimizações de performance | T4 |
| `docs/COMPATIBILITY.md` | matriz Go × SDK PL × OS × feature | T1.15 |
| `docs/CONTRIBUTING.md` | DCO, ground rules, AGENTS.md ref, EULA caveat | T0 |
| `docs/MIGRATION_FROM_GORFC.md` | mapping antigo → novo, com diff de comportamento (zero date, etc) | T4.5 |
| `examples/*/README.md` | cada exemplo descreve env, comandos | continuamente |

---

## 14. Final Recommendation

### 14.1 Escopo recomendado para v1

**v1.0 = T0 + T1 + T2.** Objetivos:

- Cliente RFC produtivo (sync, pool, session, ctx, todos os tipos ABAP, WebSocket RFC).
- Server síncrono.
- Throughput, observability OTel/slog opt-in, providers, custom IniFS.
- Auth completo (password, SNC, x509, SSO ticket).
- 13 categorias de erro tipadas.
- Build sem SDK funcional.
- macOS best-effort, Linux+Windows tier-1.
- Mín. SDK 7.50 PL12, mín. Go 1.25.

### 14.2 Escopo recomendado para v1.1 / v1.2

- **v1.1 = T3.** tRFC, qRFC, bgRFC, server bg, Repository, CLI.
- **v1.2 = T4.** Mock backend, codegen, IDoc, bridge HTTP/gRPC, migration guide.

### 14.3 O que NÃO fazer agora

- ❌ Pure-Go RFC (separadamente já justificado em [FEASIBILITY.md](FEASIBILITY.md)).
- ❌ Reverse-engineer protocolo.
- ❌ SAP Cloud SDK Go integration in-repo (deixar para projeto separado).
- ❌ Suporte a SAP-GUI ou DIAG.
- ❌ ORM / query builder.
- ❌ Driver `database/sql` para SAP.
- ❌ Distribuir SDK ou crypto lib (proibido).
- ❌ macOS no caminho crítico.

### 14.4 Principais riscos

1. **RfcCancel exato** 🟡: comportamento em invocações longas pode deixar conexão em estado inválido. Mitigação: marcar `Conn` como `broken` após cancel; descarte forçado.
2. **Server cgo callbacks** 🟡: PyRFC quebrou bgRFC; risco semelhante. Mitigação: integration tests rigorosos; T3 só após T2 server síncrono estável.
3. **Acesso a SAP de teste**: dependência humana, não técnica. Mitigação: começar por SAP NetWeaver Trial; manter GHA env protegido.
4. **EULA contributors**: funcionários de clientes SAP podem não poder contribuir RE. Mitigação: este projeto é wrapper, não RE; CONTRIBUTING.md documenta política.
5. **Compatibilidade SDK 7.50 PL × feature**: matriz precisa ser mantida 🟡; risco de claim incorreto. Mitigação: docs/SDK_FUNCTIONS_MAP.md como contrato; capability detection em runtime.
6. **mallocU/free pareamento** 🟡: leak silencioso. Mitigação: validar contra programming guide; valgrind manual em T1.5.
7. **Windows MinGW**: build flags atuais misturam MSVC/MinGW. Mitigação: flags revistas; teste em GHA windows runner; documentar `zig cc` como alternativa.
8. **Decimal precisão**: usuários esperam `decimal.Decimal` mas adapters são opt-in. Mitigação: documentar trade-off claramente; default `string`.

### 14.5 Próximo passo exato após aprovação

**Executar T0 imediatamente, em uma única sequência de PRs:**

1. **PR T0.1** — `sec: remove committed credentials, add gitignore + secret-scan workflow`. Conteúdo:
   - Apaga `gorfc/sapnwrfc.ini`.
   - Cria `gorfc/sapnwrfc.ini.example`.
   - Atualiza `.gitignore` com padrões SAP.
   - Adiciona `.github/workflows/secret-scan.yml` (gitleaks).
   - Adiciona `docs/SECURITY.md` v0 com §8 condensada.
2. **PR T0.2** — `fix: build tag windows guard`. Corrige a constraint em `gorfc/gorfc.go:1`.
3. **PR T0.3** — `fix: cgo memory leak in fillVariable defer`. Refactor do switch.
4. **PR T0.4** — `docs: add feasibility, feature matrix, sdk functions map, roadmap, architecture`. Conteúdo: documentos das §2, §3 mais este plano.

Após T0 mergeado e tagged `v0.0.1` (revival baseline), abrir PR T1.1 (`internal/backend` interface). A partir daí, a sequência da §11 pode ser executada PR a PR.

---

## Fontes consultadas

- [Go 1.25 Release Notes](https://go.dev/doc/go1.25)
- [Go Toolchains](https://go.dev/doc/toolchain)
- [SAP NWRFC SDK product page](https://support.sap.com/en/product/connectors/nwrfcsdk.html)
- [SAP KBA 3302936 — NWRFC SDK versions available](https://userapps.support.sap.com/sap/support/knowledge/en/3302936)
- [SAP node-rfc — usage](https://github.com/SAP/node-rfc/blob/main/doc/usage.md), [authentication](https://github.com/SAP/node-rfc/blob/main/doc/authentication.md)
- [PyRFC client.rst](https://github.com/SAP/PyRFC/blob/main/doc/client.rst), [server.rst](https://github.com/SAP/PyRFC/blob/main/doc/server.rst)
- [JCo JCoContext docs](https://help.hana.ondemand.com/javadoc/com/sap/conn/jco/JCoContext.html)
- [YaNco](https://github.com/dbosoft/YaNco)
- [SapNwRfc](https://github.com/huysentruitw/SapNwRfc)
- [mydoghasworms/nwrfc (Ruby)](https://github.com/mydoghasworms/nwrfc)
- [WebSocket RFC blog post](https://blogs.sap.com/2021/07/19/websocket-rfc-rfc-for-the-internet/)
- [otelslog bridge](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog)
- [golangci-lint v2 announcement](https://ldez.github.io/blog/2025/03/23/golangci-lint-v2/)

---

## Autoavaliação

- **AGENTS.md compliant?** ✅ Não há proposta de RE; SDK é provider; segredos não são commitados; falhas não silenciam (e.g., zero date explícito); platform claims gated por verificação; contribuições e testes opt-in; docs acompanham mudanças.
- **Fontes atuais?** ✅ Go 1.25 (Aug/2025), NWRFC SDK 7.50 PL18 (Dec/2025), otelslog 2026, golangci-lint v2 (Apr/2025).
- **Consolida o melhor dos wrappers?** ✅ Matriz §3 cobre 53 features cruzando 6 wrappers + gorfc atual; atribui inspiração e tier por feature.
- **Go moderno sem overengineering?** ✅ Generics apenas onde há ganho (`Call[T]`, `Decode[T]`); synctest, slog, errors.Join, iter usados onde justificados; PGO descartado em T1.
- **PRs pequenos e seguros?** ✅ §11 tem 33 PRs por tier, com rollback documentado.
- **Protege segredos?** ✅ §8 + remediação T0.1.
- **Separação paridade vs diferenciação vs experimento?** ✅ T1+T2 paridade, T3 paridade JCo, T4 diferenciação Go-native; pure-Go subset é mencionado apenas como reservado, não compromissado.
- **Critérios mensuráveis?** ✅ Cada feature tem critério (round-trip, latência, erro específico, count cov), cada PR tem aceite, cada tier tem objetivo numérico.
