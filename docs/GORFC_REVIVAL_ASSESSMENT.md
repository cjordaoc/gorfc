# gorfc Revival Assessment

> Status: assessment complete. The consolidated technical plan derived from
> this assessment is in [PLAN.md](PLAN.md). Acceptance of the plan
> supersedes the "Initial Decision" section below.

## Why Fork Instead Of Restart

The upstream project is old, but it is not disposable. It already includes:

- a CGO binding to `sapnwrfc.h`
- connection open/close and function invocation paths
- stateful and stateless connection concepts
- ABAP-to-Go datatype conversion work
- tests and examples around `STFC_STRUCTURE`
- Apache-2.0 licensing and REUSE metadata
- historical issues and pull requests from real users

The revival should preserve that base, then modernize deliberately.

## Current Strengths

- The package already targets SAP NetWeaver RFC SDK directly.
- Core ABAP datatypes are mapped in [doc/README.md](../doc/README.md).
- The code has a real CGO bridge instead of a speculative protocol rewrite.
- The README documents that users must install SAP NWRFC SDK separately.
- The code already models SDK and Go-side errors separately.

## Current Gaps Identified

- Windows support is documented as incomplete, but the build constraint in
  [gorfc/gorfc.go:1](../gorfc/gorfc.go#L1) reads
  `(linux && cgo) || (amd64 && cgo) || (darwin && cgo)`, which lets the
  package build on any `amd64` platform — including Windows where it does
  not work. Build tag is wrong and must be tightened.
- CGO include and library paths are hardcoded for common local SDK
  locations. Build configuration is not portable.
- The public API predates modern Go context / cancellation conventions.
  No `context.Context` anywhere; nothing can be cancelled or timed out
  at the call level.
- The module path still points at `github.com/sap/gorfc` even though
  the upstream is archived.
- Tests appear to require the SAP NWRFC SDK and / or SAP connectivity;
  there is no SDK-free test suite and no opt-in gating for integration
  tests.
- Credential handling, logging guidance, and integration-test isolation
  need to be tightened. The repository historically shipped
  `gorfc/sapnwrfc.ini` with real credentials and SAP corporate hostnames;
  removal and `.gitignore` are mandatory.
- Error typing should be reviewed against current `node-rfc`, PyRFC, and
  SAP NWRFC SDK behavior. PyRFC's eight-category taxonomy is more useful
  than the current binary `RfcError` / `GoRfcError` split.
- Memory ownership across CGO boundaries needs focused review:
  - `defer C.free(unsafe.Pointer(cValue))` and the same for `bValue` in
    [gorfc/gorfc.go:187-188](../gorfc/gorfc.go#L187-L188) capture the
    pointer **before** the switch assigns to it. The deferred free runs
    on the original `nil` pointer, leaking the memory actually allocated
    by `fillString` / `C.CBytes`.
  - `connectionFinalizer` runs in an arbitrary goroutine via
    `runtime.SetFinalizer`. Frees C memory whileClaude, preciso que você aja como arquiteto sênior Go + SAP NetWeaver RFC SDK + engenharia de wrappers nativos, obedecendo estritamente ao AGENTS.md do repositório.

OBJETIVO
Quero que você produza um PLANO TÉCNICO DE CONSOLIDAÇÃO para transformar o wrapper RFC em Go em um wrapper moderno, Go-native, robusto e superior ao estado atual dos wrappers estudados.

Você NÃO deve sair codando ainda, salvo se o AGENTS.md exigir alguma inspeção mínima. Primeiro, entregue um plano completo, verificável e acionável.

CONTEXTO DO ESTUDO JÁ REALIZADO
Você já avaliou os seguintes wrappers/bibliotecas como referência:

1. SAP JCo
2. node-rfc
3. PyRFC
4. YaNco
5. SapNwRfc
6. Ruby nwrfc
7. gorfc atual

Conclusões principais do estudo:
- JCo é a referência de cobertura funcional: sRFC, tRFC, qRFC, bgRFC, IDoc, server, metadata repository, serializações e transportes.
- node-rfc é referência de API moderna: async/Promise, cancel, timeout, customFs, BCD/date/time pluggáveis, WebSocket RFC.
- PyRFC é referência de taxonomia de erros: ABAPApplicationError, ABAPRuntimeError, LogonError, CommunicationError, ExternalAuthorizationError, ExternalApplicationError, ExternalRuntimeError, RFCError.
- YaNco, SapNwRfc e Ruby nwrfc trazem pontos úteis de DI/runtime abstraction, DTO mapping, BigDecimal/BCD e server/client patterns.
- O alvo Go deve consolidar o melhor de todos em um único wrapper.
- O SAP NetWeaver RFC SDK já expõe a maior parte da capacidade via C API; o trabalho real é design de wrapper, marshaling, lifecycle, ergonomia, segurança, testes e observabilidade.
- O estudo propôs tiers:
  - T1: cliente RFC sólido para produção.
  - T2: throughput, async parity, helpers, observabilidade, custom FS e providers.
  - T3: tRFC/qRFC/bgRFC/server/repository/runtime backend.
  - T4: diferenciação Go-native: codegen, IDoc package, mock backend, CLI, bridge HTTP/gRPC, helpers SAP auth.

FONTES E PESQUISA OBRIGATÓRIA
Antes de finalizar o plano, pesquise na web e valide contra fontes atuais e primárias sempre que possível:

1. Documentação oficial Go mais recente.
   - Considere Go 1.25+ ou a versão estável mais recente disponível no momento da execução.
   - Verifique recursos modernos relevantes:
     - context.Context best practices
     - errors.Join / wrapping / Is / As
     - slog
     - testing/fuzz
     - testing/synctest, se aplicável
     - generics/type parameters
     - slices/maps/cmp/iter, se aplicável
     - toolchain directive
     - workspaces
     - PGO
     - race detector
     - govulncheck
     - cgo rules atuais
     - cross-compilation constraints
     - build tags
     - module layout moderno
     - OpenTelemetry Go atual
     - golangci-lint ou alternativa atual recomendada

2. SAP NetWeaver RFC SDK atual.
   - Valide o máximo possível contra documentação SAP oficial.
   - Confirme limites por versão:
     - WebSocket RFC
     - bgRFC
     - throughput
     - UTCLONG
     - FClaude, preciso que você aja como arquiteto sênior Go + SAP NetWeaver RFC SDK + engenharia de wrappers nativos, obedecendo estritamente ao AGENTS.md do repositório.

OBJETIVO
Quero que você produza um PLANO TÉCNICO DE CONSOLIDAÇÃO para transformar o wrapper RFC em Go em um wrapper moderno, Go-native, robusto e superior ao estado atual dos wrappers estudados.

Você NÃO deve sair codando ainda, salvo se o AGENTS.md exigir alguma inspeção mínima. Primeiro, entregue um plano completo, verificável e acionável.

CONTEXTO DO ESTUDO JÁ REALIZADO
Você já avaliou os seguintes wrappers/bibliotecas como referência:

1. SAP JCo
2. node-rfc
3. PyRFC
4. YaNco
5. SapNwRfc
6. Ruby nwrfc
7. gorfc atual

Conclusões principais do estudo:
- JCo é a referência de cobertura funcional: sRFC, tRFC, qRFC, bgRFC, IDoc, server, metadata repository, serializações e transportes.
- node-rfc é referência de API moderna: async/Promise, cancel, timeout, customFs, BCD/date/time pluggáveis, WebSocket RFC.
- PyRFC é referência de taxonomia de erros: ABAPApplicationError, ABAPRuntimeError, LogonError, CommunicationError, ExternalAuthorizationError, ExternalApplicationError, ExternalRuntimeError, RFCError.
- YaNco, SapNwRfc e Ruby nwrfc trazem pontos úteis de DI/runtime abstraction, DTO mapping, BigDecimal/BCD e server/client patterns.
- O alvo Go deve consolidar o melhor de todos em um único wrapper.
- O SAP NetWeaver RFC SDK já expõe a maior parte da capacidade via C API; o trabalho real é design de wrapper, marshaling, lifecycle, ergonomia, segurança, testes e observabilidade.
- O estudo propôs tiers:
  - T1: cliente RFC sólido para produção.
  - T2: throughput, async parity, helpers, observabilidade, custom FS e providers.
  - T3: tRFC/qRFC/bgRFC/server/repository/runtime backend.
  - T4: diferenciação Go-native: codegen, IDoc package, mock backend, CLI, bridge HTTP/gRPC, helpers SAP auth.

FONTES E PESQUISA OBRIGATÓRIA
Antes de finalizar o plano, pesquise na web e valide contra fontes atuais e primárias sempre que possível:

1. Documentação oficial Go mais recente.
   - Considere Go 1.25+ ou a versão estável mais recente disponível no momento da execução.
   - Verifique recursos modernos relevantes:
     - context.Context best practices
     - errors.Join / wrapping / Is / As
     - slog
     - testing/fuzz
     - testing/synctest, se aplicável
     - generics/type parameters
     - slices/maps/cmp/iter, se aplicável
     - toolchain directive
     - workspaces
     - PGO
     - race detector
     - govulncheck
     - cgo rules atuais
     - cross-compilation constraints
     - build tags
     - module layout moderno
     - OpenTelemetry Go atual
     - golangci-lint ou alternativa atual recomendada

2. SAP NetWeaver RFC SDK atual.
   - Valide o máximo possível contra documentação SAP oficial.
   - Confirme limites por versão:
     - WebSocket RFC
     - bgRFC
     - throughput
     - UTCLONG
     - Fast Serialization/cbRfc
     - server callbacks
     - SNC/SAML/X509/SSO
     - IDoc/sapidoc, se separado
   - Não invente disponibilidade de função. Onde não for possível confirmar, marque como “requires verification”.

3. Wrappers de referência.
   - Revalide node-rfc, PyRFC, SAP JCo, YaNco, SapNwRfc, Ruby nwrfc e gorfc atual.
   - Não copie código. Use apenas para entender API, semântica, lifecycle, erros, edge cases e gaps.

REGRAS DE EXECUÇÃO
1. Obedeça ao AGENTS.md integralmente.
2. Não implemente nada ainda sem antes entregar o plano.
3. Não faça simplificações apressadas.
4. Não transforme o plano em uma lista genérica. Quero decisões concretas, trade-offs e critérios de aceite.
5. Quando houver incerteza técnica, marque explicitamente:
   - Confirmado por fonte
   - Inferido com base no SDK/wrapper
   - Requer validação em SAP real
   - Requer validação por versão do SDK
6. Não esconda riscos.
7. Não use placeholders.
8. Não proponha reverse engineering de protocolo RFC. O wrapper deve usar o SAP NetWeaver RFC SDK.
9. Segurança é requisito de primeira classe:
   - Nunca logar senha, ticket, SSO, certificado, SNC credentials, sapnwrfc.ini sensível.
   - Redação automática de segredos em logs/spans/errors.
   - Política clara para sapnwrfc.ini, environment variables e Kubernetes secrets/configmaps.
10. Licenciamento e redistribuição:
   - Documentar claramente que SAP NWRFC SDK e sapcrypto/CommonCryptoLib são componentes proprietários fornecidos pelo cliente/usuário.
   - Não empacotar binários proprietários no repositório ou releases.
11. Windows/Linux/macOS:
   - Definir o suporte por plataforma.
   - Explicitar limitações de cgo, MinGW/MSVC/zig cc, LD_LIBRARY_PATH, PATH, DYLD_LIBRARY_PATH etc.
12. Testes:
   - Separar testes SDK-free, testes com SDK instalado e testes de integração contra SAP real.
   - Criar plano de conformance com STFC_CONNECTION, RFC_PING, STFC_STRUCTURE e BAPIs reais quando possível.
   - Server/bgRFC/qRFC/tRFC exigem cenários SAP próprios e devem ter critérios claros.

ENTREGÁVEL PRINCIPAL
Produza um plano técnico com a seguinte estrutura obrigatória:

# 1. Executive Summary
Explique, em termos objetivos, como consolidar o melhor das bibliotecas estudadas em um único wrapper Go.

# 2. Target Architecture
Desenhe a arquitetura proposta em texto, incluindo pacotes/módulos como:

- core connection/client
- config/destination
- pool
- metadata repository
- function/type descriptors
- marshaling/unmarshaling
- ABAP type system
- errors
- transactions: tRFC/qRFC/bgRFC
- server runtime
- observability
- security/redaction
- mock backend
- codegen
- optional IDoc package
- CLI
- internal cgo binding layer

Explique as fronteiras entre API pública e internal/cgo.

# 3. Consolidation Matrix
Transforme a matriz do estudo em uma matriz final de decisão:

Para cada feature:
- Fonte principal de inspiração: JCo, node-rfc, PyRFC, YaNco, SapNwRfc, Ruby nwrfc, gorfc
- Status no gorfc atual
- Status alvo
- Tier sugerido
- SDK functions envolvidas
- Risco
- Critério de aceite
- Necessita SAP real? Sim/Não
- Necessita versão mínima do SDK? Qual?

Inclua pelo menos estas features:
- sRFC client
- direct connection
- load-balanced connection
- sapnwrfc.ini
- custom destination provider
- custom server provider
- connection pool
- explicit stateful session
- inbound RFC server
- tRFC client/server
- qRFC client/server
- bgRFC client/server
- IDoc
- function/type metadata
- metadata cache/invalidate
- repository abstraction
- throughput
- password auth
- SSO ticket
- X509
- SNC
- SAML/Bearer, se suportado
- WebSocket RFC
- CPIC
- RfcCancel via context.Context
- per-call timeout
- notRequested / RfcSetParameterActive
- direction filters
- BCD/decimal policy
- date/time policy
- strict date/time toggles
- rstrip
- return_import_params
- struct mapping via tags
- custom FS for sapnwrfc.ini
- runtime ini reload
- crypto library loading
- RfcResetServerContext
- language ISO/SAP conversion
- trace control
- typed error hierarchy
- library presence check
- build tags nosdk
- async/non-blocking API idiomática em Go
- runtime/backend abstraction
- connection event listeners
- codegen
- OpenTelemetry/slog
- mock backend

# 4. Go Modernization Plan
Explique como usar os recursos modernos da linguagem e ecossistema Go para melhorar o wrapper:

- context.Context para timeout, cancel e lifecycle
- goroutines sem “fake async”
- channel/errgroup quando fizer sentido
- errors.Is/As, wrapping e typed errors
- errors.Join quando houver múltiplos erros de cleanup
- slog com redaction de segredos
- generics apenas onde houver ganho real
- struct tags rfc:""
- constraints para tipos ABAP, se aplicável
- decimal strategy: string, big.Rat, shopspring/decimal ou alternativa, com trade-off
- time/date strategy e validação ABAP DATS/TIMS/UTCLONG
- iter/slices/maps/cmp se aplicável na versão Go alvo
- testing/fuzz para marshaling/unmarshaling
- race detector para pool/server
- govulncheck
- PGO, se aplicável
- build constraints para cgo/sdk/nosdk
- toolchain directive
- workspace/multi-module, se necessário
- API idiomática Go e compatível com Go 1 promise

Não use modismo. Explique onde cada recurso agrega valor e onde NÃO deve ser usado.

# 5. Public API Proposal
Proponha uma API Go idiomática, com exemplos de uso, para:

1. Abrir conexão direta
2. Abrir conexão via destination
3. Pool
4. Call simples
5. Call tipada com struct tags
6. Metadata describe
7. Stateful session
8. Context timeout/cancel
9. notRequested
10. BAPIRET2 helper
11. tRFC
12. qRFC
13. bgRFC
14. Server handler
15. Throughput metrics
16. OpenTelemetry opt-in
17. Mock backend
18. Codegen

Os exemplos devem ser completos o suficiente para orientar implementação, mas ainda sem modificar o código.

# 6. Internal cgo Binding Strategy
Defina:
- Como encapsular RFC_CONNECTION_HANDLE, RFC_FUNCTION_HANDLE etc.
- Ownership e lifecycle de handles
- finalizers: usar ou evitar? justificar
- conversão SAPUC/UTF-8
- error_info extraction
- memory safety
- thread safety
- callbacks de server
- ponte entre callbacks C e handlers Go
- riscos de chamadas Go a partir de C
- build tags
- graceful ErrSDKUnavailable quando SDK ausente
- como detectar versão/capabilities do SDK

# 7. Error Taxonomy
Crie uma hierarquia Go inspirada em PyRFC/JCo/node-rfc:

- RFCError base
- ABAPApplicationError
- ABAPRuntimeError
- LogonError
- CommunicationError
- ExternalAuthorizationError
- ExternalApplicationError
- ExternalRuntimeError
- ABAPClassicException
- ABAPClassException
- SDKUnavailableError
- UnsupportedFeatureError
- BrokenConnectionError
- TimeoutError
- CancelledError

Para cada erro:
- quando ocorre
- dados carregados de RFC_ERROR_INFO
- fields públicos
- compatibilidade com errors.Is/As
- redaction

# 8. Security Model
Defina política de segurança para:
- credentials
- sapnwrfc.ini
- environment variables
- SNC
- X509
- MYSAPSSO2
- SAML/Bearer
- trace
- logs
- OpenTelemetry spans
- panic recovery
- examples/docs
- CI
- sample configs sem segredos
- secret scanning

Inclua redaction rules objetivas.

# 9. Testing and Conformance Strategy
Divida em:

A. SDK-free unit tests
- ABAP type conversion
- date/time validation
- BCD/decimal policy
- struct tag mapping
- error wrapping
- redaction
- config parsing

B. SDK-present local tests
- EnsureLibraryPresent
- dynamic loading/cgo
- version/capability detection

C. SAP integration tests
- RFC_PING
- STFC_CONNECTION
- STFC_STRUCTURE
- metadata
- BAPIRET2 helper
- stateful session
- cancel/timeout
- pool
- tRFC/qRFC/bgRFC
- server callbacks

D. CI strategy
- what runs on PR
- what runs nightly
- what requires secrets/SAP sandbox
- Windows/Linux/macOS matrix
- race detector
- fuzzing
- govulncheck
- linter

# 10. Roadmap by Tiers
Reescreva os tiers T1–T4 em backlog executável.

Para cada tier:
- Objetivo
- Entregáveis
- Pacotes alterados/criados
- Features
- Critérios de aceite
- Testes mínimos
- Riscos
- Dependências externas
- O que fica explicitamente fora

T1 precisa resultar em cliente produtivo para BAPI/sRFC.
T2 precisa trazer observabilidade, providers, throughput e API moderna.
T3 precisa trazer protocolos avançados e server.
T4 precisa trazer diferenciação Go-native.

# 11. Implementation Sequence
Crie uma sequência de PRs pequenos e seguros.

Para cada PR:
- Nome
- Objetivo
- Arquivos/pacotes prováveis
- Mudanças esperadas
- Testes
- Critérios de aceite
- Risco de regressão
- Rollback strategy

Não quero um PR gigante. Quero uma sequência que permita revisão incremental.

# 12. Decision Log
Liste decisões pendentes e proponha recomendação para cada uma:

1. IDoc no core, pacote separado ou fora do v1?
2. WebSocket RFC obrigatório no T1 ou capability-based?
3. Server-side no T1 reduzido ou T3 completo?
4. Codegen no T4 ou roadmap separado?
5. OpenTelemetry/slog no core ou subpacote opt-in?
6. Mock backend como subpacote ou projeto separado?
7. macOS suportado ou best-effort?
8. Estratégia de SAP test system
9. CLA/DCO
10. Versão mínima de Go
11. Versão mínima de SAP NWRFC SDK
12. Política de compatibilidade semântica da API pública

Para cada decisão:
- Opções
- Recomendação
- Justificativa
- Impacto
- Risco

# 13. Documentation Plan
Proponha documentação obrigatória:

- docs/FEATURE_MATRIX.md
- docs/SDK_FUNCTIONS_MAP.md
- docs/ROADMAP.md
- docs/SECURITY.md
- docs/CONFIGURATION.md
- docs/ERRORS.md
- docs/TESTING.md
- docs/CONTRIBUTING.md
- examples/
- migration guide se houver API legada

# 14. Final Recommendation
Feche com:
- Escopo recomendado para v1
- Escopo recomendado para v1.1/v1.2
- O que não vale a pena fazer agora
- Principais riscos
- Próximo passo exato após aprovação do plano

IMPORTANTE
Sua resposta deve ser técnica, objetiva e profunda. Não quero uma resposta curta. Não quero “vamos implementar X” sem dizer como será validado. Não quero placeholders. Não quero assumir que uma função do SDK existe sem validar ou marcar como “requires verification”.

Antes de entregar, faça uma autoavaliação:
- O plano está conforme AGENTS.md?
- O plano usa fontes atuais?
- O plano consolida de fato o melhor de JCo/node-rfc/PyRFC/YaNco/SapNwRfc/Ruby nwrfc/gorfc?
- O plano usa Go moderno sem overengineering?
- O plano é implementável em PRs pequenos?
- O plano protege segredos SAP?
- O plano separa claramente paridade, diferenciação e experimentos?
- O plano tem critérios de aceite mensuráveis?

Entregue o plano final em Markdown.ast Serialization/cbRfc
     - server callbacks
     - SNC/SAML/X509/SSO
     - IDoc/sapidoc, se separado
   - Não invente disponibilidade de função. Onde não for possível confirmar, marque como “requires verification”.

3. Wrappers de referência.
   - Revalide node-rfc, PyRFC, SAP JCo, YaNco, SapNwRfc, Ruby nwrfc e gorfc atual.
   - Não copie código. Use apenas para entender API, semântica, lifecycle, erros, edge cases e gaps.

REGRAS DE EXECUÇÃO
1. Obedeça ao AGENTS.md integralmente.
2. Não implemente nada ainda sem antes entregar o plano.
3. Não faça simplificações apressadas.
4. Não transforme o plano em uma lista genérica. Quero decisões concretas, trade-offs e critérios de aceite.
5. Quando houver incerteza técnica, marque explicitamente:
   - Confirmado por fonte
   - Inferido com base no SDK/wrapper
   - Requer validação em SAP real
   - Requer validação por versão do SDK
6. Não esconda riscos.
7. Não use placeholders.
8. Não proponha reverse engineering de protocolo RFC. O wrapper deve usar o SAP NetWeaver RFC SDK.
9. Segurança é requisito de primeira classe:
   - Nunca logar senha, ticket, SSO, certificado, SNC credentials, sapnwrfc.ini sensível.
   - Redação automática de segredos em logs/spans/errors.
   - Política clara para sapnwrfc.ini, environment variables e Kubernetes secrets/configmaps.
10. Licenciamento e redistribuição:
   - Documentar claramente que SAP NWRFC SDK e sapcrypto/CommonCryptoLib são componentes proprietários fornecidos pelo cliente/usuário.
   - Não empacotar binários proprietários no repositório ou releases.
11. Windows/Linux/macOS:
   - Definir o suporte por plataforma.
   - Explicitar limitações de cgo, MinGW/MSVC/zig cc, LD_LIBRARY_PATH, PATH, DYLD_LIBRARY_PATH etc.
12. Testes:
   - Separar testes SDK-free, testes com SDK instalado e testes de integração contra SAP real.
   - Criar plano de conformance com STFC_CONNECTION, RFC_PING, STFC_STRUCTURE e BAPIs reais quando possível.
   - Server/bgRFC/qRFC/tRFC exigem cenários SAP próprios e devem ter critérios claros.

ENTREGÁVEL PRINCIPAL
Produza um plano técnico com a seguinte estrutura obrigatória:

# 1. Executive Summary
Explique, em termos objetivos, como consolidar o melhor das bibliotecas estudadas em um único wrapper Go.

# 2. Target Architecture
Desenhe a arquitetura proposta em texto, incluindo pacotes/módulos como:

- core connection/client
- config/destination
- pool
- metadata repository
- function/type descriptors
- marshaling/unmarshaling
- ABAP type system
- errors
- transactions: tRFC/qRFC/bgRFC
- server runtime
- observability
- security/redaction
- mock backend
- codegen
- optional IDoc package
- CLI
- internal cgo binding layer

Explique as fronteiras entre API pública e internal/cgo.

# 3. Consolidation Matrix
Transforme a matriz do estudo em uma matriz final de decisão:

Para cada feature:
- Fonte principal de inspiração: JCo, node-rfc, PyRFC, YaNco, SapNwRfc, Ruby nwrfc, gorfc
- Status no gorfc atual
- Status alvo
- Tier sugerido
- SDK functions envolvidas
- Risco
- Critério de aceite
- Necessita SAP real? Sim/Não
- Necessita versão mínima do SDK? Qual?

Inclua pelo menos estas features:
- sRFC client
- direct connection
- load-balanced connection
- sapnwrfc.ini
- custom destination provider
- custom server provider
- connection pool
- explicit stateful session
- inbound RFC server
- tRFC client/server
- qRFC client/server
- bgRFC client/server
- IDoc
- function/type metadata
- metadata cache/invalidate
- repository abstraction
- throughput
- password auth
- SSO ticket
- X509
- SNC
- SAML/Bearer, se suportado
- WebSocket RFC
- CPIC
- RfcCancel via context.Context
- per-call timeout
- notRequested / RfcSetParameterActive
- direction filters
- BCD/decimal policy
- date/time policy
- strict date/time toggles
- rstrip
- return_import_params
- struct mapping via tags
- custom FS for sapnwrfc.ini
- runtime ini reload
- crypto library loading
- RfcResetServerContext
- language ISO/SAP conversion
- trace control
- typed error hierarchy
- library presence check
- build tags nosdk
- async/non-blocking API idiomática em Go
- runtime/backend abstraction
- connection event listeners
- codegen
- OpenTelemetry/slog
- mock backend

# 4. Go Modernization Plan
Explique como usar os recursos modernos da linguagem e ecossistema Go para melhorar o wrapper:

- context.Context para timeout, cancel e lifecycle
- goroutines sem “fake async”
- channel/errgroup quando fizer sentido
- errors.Is/As, wrapping e typed errors
- errors.Join quando houver múltiplos erros de cleanup
- slog com redaction de segredos
- generics apenas onde houver ganho real
- struct tags rfc:""
- constraints para tipos ABAP, se aplicável
- decimal strategy: string, big.Rat, shopspring/decimal ou alternativa, com trade-off
- time/date strategy e validação ABAP DATS/TIMS/UTCLONG
- iter/slices/maps/cmp se aplicável na versão Go alvo
- testing/fuzz para marshaling/unmarshaling
- race detector para pool/server
- govulncheck
- PGO, se aplicável
- build constraints para cgo/sdk/nosdk
- toolchain directive
- workspace/multi-module, se necessário
- API idiomática Go e compatível com Go 1 promise

Não use modismo. Explique onde cada recurso agrega valor e onde NÃO deve ser usado.

# 5. Public API Proposal
Proponha uma API Go idiomática, com exemplos de uso, para:

1. Abrir conexão direta
2. Abrir conexão via destination
3. Pool
4. Call simples
5. Call tipada com struct tags
6. Metadata describe
7. Stateful session
8. Context timeout/cancel
9. notRequested
10. BAPIRET2 helper
11. tRFC
12. qRFC
13. bgRFC
14. Server handler
15. Throughput metrics
16. OpenTelemetry opt-in
17. Mock backend
18. Codegen

Os exemplos devem ser completos o suficiente para orientar implementação, mas ainda sem modificar o código.

# 6. Internal cgo Binding Strategy
Defina:
- Como encapsular RFC_CONNECTION_HANDLE, RFC_FUNCTION_HANDLE etc.
- Ownership e lifecycle de handles
- finalizers: usar ou evitar? justificar
- conversão SAPUC/UTF-8
- error_info extraction
- memory safety
- thread safety
- callbacks de server
- ponte entre callbacks C e handlers Go
- riscos de chamadas Go a partir de C
- build tags
- graceful ErrSDKUnavailable quando SDK ausente
- como detectar versão/capabilities do SDK

# 7. Error Taxonomy
Crie uma hierarquia Go inspirada em PyRFC/JCo/node-rfc:

- RFCError base
- ABAPApplicationError
- ABAPRuntimeError
- LogonError
- CommunicationError
- ExternalAuthorizationError
- ExternalApplicationError
- ExternalRuntimeError
- ABAPClassicException
- ABAPClassException
- SDKUnavailableError
- UnsupportedFeatureError
- BrokenConnectionError
- TimeoutError
- CancelledError

Para cada erro:
- quando ocorre
- dados carregados de RFC_ERROR_INFO
- fields públicos
- compatibilidade com errors.Is/As
- redaction

# 8. Security Model
Defina política de segurança para:
- credentials
- sapnwrfc.ini
- environment variables
- SNC
- X509
- MYSAPSSO2
- SAML/Bearer
- trace
- logs
- OpenTelemetry spans
- panic recovery
- examples/docs
- CI
- sample configs sem segredos
- secret scanning

Inclua redaction rules objetivas.

# 9. Testing and Conformance Strategy
Divida em:

A. SDK-free unit tests
- ABAP type conversion
- date/time validation
- BCD/decimal policy
- struct tag mapping
- error wrapping
- redaction
- config parsing

B. SDK-present local tests
- EnsureLibraryPresent
- dynamic loading/cgo
- version/capability detection

C. SAP integration tests
- RFC_PING
- STFC_CONNECTION
- STFC_STRUCTURE
- metadata
- BAPIRET2 helper
- stateful session
- cancel/timeout
- pool
- tRFC/qRFC/bgRFC
- server callbacks

D. CI strategy
- what runs on PR
- what runs nightly
- what requires secrets/SAP sandbox
- Windows/Linux/macOS matrix
- race detector
- fuzzing
- govulncheck
- linter

# 10. Roadmap by Tiers
Reescreva os tiers T1–T4 em backlog executável.

Para cada tier:
- Objetivo
- Entregáveis
- Pacotes alterados/criados
- Features
- Critérios de aceite
- Testes mínimos
- Riscos
- Dependências externas
- O que fica explicitamente fora

T1 precisa resultar em cliente produtivo para BAPI/sRFC.
T2 precisa trazer observabilidade, providers, throughput e API moderna.
T3 precisa trazer protocolos avançados e server.
T4 precisa trazer diferenciação Go-native.

# 11. Implementation Sequence
Crie uma sequência de PRs pequenos e seguros.

Para cada PR:
- Nome
- Objetivo
- Arquivos/pacotes prováveis
- Mudanças esperadas
- Testes
- Critérios de aceite
- Risco de regressão
- Rollback strategy

Não quero um PR gigante. Quero uma sequência que permita revisão incremental.

# 12. Decision Log
Liste decisões pendentes e proponha recomendação para cada uma:

1. IDoc no core, pacote separado ou fora do v1?
2. WebSocket RFC obrigatório no T1 ou capability-based?
3. Server-side no T1 reduzido ou T3 completo?
4. Codegen no T4 ou roadmap separado?
5. OpenTelemetry/slog no core ou subpacote opt-in?
6. Mock backend como subpacote ou projeto separado?
7. macOS suportado ou best-effort?
8. Estratégia de SAP test system
9. CLA/DCO
10. Versão mínima de Go
11. Versão mínima de SAP NWRFC SDK
12. Política de compatibilidade semântica da API pública

Para cada decisão:
- Opções
- Recomendação
- Justificativa
- Impacto
- Risco

# 13. Documentation Plan
Proponha documentação obrigatória:

- docs/FEATURE_MATRIX.md
- docs/SDK_FUNCTIONS_MAP.md
- docs/ROADMAP.md
- docs/SECURITY.md
- docs/CONFIGURATION.md
- docs/ERRORS.md
- docs/TESTING.md
- docs/CONTRIBUTING.md
- examples/
- migration guide se houver API legada

# 14. Final Recommendation
Feche com:
- Escopo recomendado para v1
- Escopo recomendado para v1.1/v1.2
- O que não vale a pena fazer agora
- Principais riscos
- Próximo passo exato após aprovação do plano

IMPORTANTE
Sua resposta deve ser técnica, objetiva e profunda. Não quero uma resposta curta. Não quero “vamos implementar X” sem dizer como será validado. Não quero placeholders. Não quero assumir que uma função do SDK existe sem validar ou marcar como “requires verification”.

Antes de entregar, faça uma autoavaliação:
- O plano está conforme AGENTS.md?
- O plano usa fontes atuais?
- O plano consolida de fato o melhor de JCo/node-rfc/PyRFC/YaNco/SapNwRfc/Ruby nwrfc/gorfc?
- O plano usa Go moderno sem overengineering?
- O plano é implementável em PRs pequenos?
- O plano protege segredos SAP?
- O plano separa claramente paridade, diferenciação e experimentos?
- O plano tem critérios de aceite mensuráveis?

Entregue o plano final em Markdown. the connection may still
    be in use. Race risk.
  - In `ConnectionFromParams`, the loop that fills connection parameters
    overwrites `err` on every iteration; only the last error survives.
- ABAP `RFCTYPE_DATE` returns `nil` silently for the `00000000` initial
  date in [gorfc/gorfc.go:833-835](../gorfc/gorfc.go#L833-L835). This is
  a silent fallback, which violates the no-silent-fallback rule in
  [AGENTS.md](../AGENTS.md). The new design fails explicitly with an
  `ErrZeroDate` and only suppresses it when `AllowZeroDate` is set on
  call options.
- `Connection` is not goroutine-safe. The SAP NWRFC SDK requires that a
  single `RFC_CONNECTION_HANDLE` is used by only one thread at a time.
  There is no mutex.
- No connection pool, no metadata cache control, no throughput tracking,
  no inbound server, no tRFC / qRFC / bgRFC support, no IDoc helpers.

The detailed bug-and-gap audit is in [PLAN.md](PLAN.md) §1.3, with line
references and severity.

## Initial Decision (superseded)

> The original decision recorded here was: keep this fork as the
> evaluation and revival base; do not rewrite until the existing API,
> issue history, platform gaps, CGO ownership, and node-rfc parity
> requirements are mapped.
>
> That work is now complete. The consolidated technical plan in
> [PLAN.md](PLAN.md) is the next step. The remediation work in Tier 0 of
> the plan addresses the security and build issues called out above
> before any new code lands.
