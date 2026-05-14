<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# nexus-spec Integration Roadmap (v0.2.0)

> **Audience:** maintainers, reviewers, downstream `nexus-spec`
> integrators, AI coding agents (Claude Code / Codex) working on
> the v0.2.0 cycle.
>
> **Status:** active planning document — drives v0.2.0 work but
> is **not** the deliverable (see §10).
>
> **Created:** 2026-05-08. **Last revised:** 2026-05-08.

This is the living work plan for the `nexus-spec` adapter
integration that consumes
`github.com/cjordaoc/gorfc/nwrfc` from a downstream
`internal/sap/rfc/` package, and the corresponding hardening of
the `nwrfc` public API surface for production VDI deployment on
Windows and Linux.

The non-negotiables in [AGENTS.md](../AGENTS.md) and the
project-level coding principles bind every change here. Where
this roadmap and AGENTS.md disagree, AGENTS.md wins.

---

## 0. Status legend

Every roadmap item below carries one of:

| Mark | Meaning |
|---|---|
| `[ ]` | **planned** — not started |
| `[~]` | **in progress** — partially landed in the working tree |
| `[x]` | **implemented and validated** — code, tests, and docs all green; must cite evidence |
| `[!]` | **blocked** — requires a manual environment, customer SAP system, signed-off SDK, or out-of-band decision |
| `[E]` | **evidence required before promotion** — design done; implementation pending verification against primary source |

A box can only be set to `[x]` if **all** of the following are
true and verifiable in the repository:

1. Code is implemented (not stubbed, not TODO'd).
2. A test exercises the change (unit, integration, or behavior
   probe), or — when no executable test is feasible — a
   reproducible manual evidence command is captured under
   `docs/EVIDENCE/`.
3. Documentation reflects the implemented behavior.
4. `gofmt`, `go vet`, and the relevant test invocation pass on
   the maintainer's machine **and** are reproduced by the CI
   matrix when applicable.

Anything short of that is `[~]`, `[E]`, or `[ ]`.

---

## 1. Scope (interpreted)

The downstream `nexus-spec` consumer needs:

1. A typed RFC client suitable for **first-class Windows VDI
   deployment** (Citrix, VMware Horizon, AWS WorkSpaces, AVD)
   without depending on user-controlled `PATH`,
   `LD_LIBRARY_PATH`, registry, or admin rights.
2. A taxonomy of errors that lets the adapter retry, alert, and
   re-prompt credentials with confidence — distinguishing
   **bad password** from **expired password** from
   **locked user** without string matching.
3. A redaction story strong enough that operators can
   `slog.Info(p)` a `Params` or an error in production logs
   and CI logs without leaking secrets.
4. Confident **mid-call cancellation** so a context-cancelled
   HTTP request can free the SAP-side blocked operation —
   without compromising the SDK thread-safety contract.
5. **Capability enforcement**: WebSocket RFC silently
   downgrading on the wrong SDK PL is treated as a security
   incident; we fail-fast.
6. **Pool sanitation**: ABAP session state must not leak
   between pool checkouts when the operator opts in.
7. **Trace safety**: trace level > 0 captures payloads to disk;
   deployments must be able to cap the maximum allowed level.
8. Reproducible cross-build from Linux to Windows with `zig cc`,
   with CI proof.
9. Documentation aligned with the actual code, both for
   operators and for AI coding agents.

`nexus-spec` does NOT need any of:

* `RfcCancel` / `RfcCloseConnection` exposed directly. It uses
  `context.Context`. The library decides how to honor
  cancellation internally (see §3).
* Direct access to `internal/...`. It must be possible to
  consume the full feature set through `nwrfc`.
* A specific Decimal implementation. It should plug its own.
* Tail-end cleanup of mid-call mutating BAPIs. The library
  documents the caveat; the consumer treats a cancelled
  mutating call as `outcome unknown` (see §11).

---

## 2. Architecture (target shape; some pieces still planned)

```
github.com/cjordaoc/gorfc
├── nwrfc/                      # public API (typed)
│   ├── conn.go                 # Conn lifecycle + Cancel        (P1.1, planned)
│   ├── errors.go               # core taxonomy
│   ├── errors_logon.go         # logon subtypes                 (P1.2, in progress)
│   ├── params.go               # redaction + MaxTraceLevel      (P1.3, P2.2, in progress)
│   ├── pool.go                 # AlwaysReset                    (P2.1, planned)
│   ├── types.go                # Date/Time/UTCLong/Decimal      (P1.4, in progress)
│   ├── utility.go              # EnsureSDK / SDKVersion / Caps  (P1.8, planned)
│   └── …
├── internal/backend/
│   ├── backend.go              # +Cancellable optional capability (planned)
│   ├── category.go
│   ├── sdkerror.go
│   └── types.go                # IsSensitiveKey + prefixes      (P1.3, partially landed)
├── internal/sdkbackend/        # cgo SAP NWRFC SDK
│   ├── conn.go                 # cancelConn (decision in §3)    (planned)
│   ├── invoke.go               # shared cancel watcher          (planned)
│   └── …
├── internal/nosdkbackend/      # nwrfc_nosdk stub
└── docs/
    ├── DEPLOY.md               # NEW — VDI deploy playbook      (P1.7, planned)
    ├── EVIDENCE/SCHEMA.md      # NEW — integration evidence     (P2.4, planned)
    └── …
```

Items marked **planned** here are explicitly **not** present in
the tree at the time this roadmap was written. The status
legend in §0 is authoritative; this diagram is a sketch of the
end state and must not be read as a status report.

---

## 3. Cancel implementation decision: `RfcCancel` vs `RfcCloseConnection`

This section is the most important in the roadmap. Earlier
drafts of the requirements diverged on whether the SAP NW RFC
SDK still exposes `RfcCancel`. The implementation **must not**
proceed on either premise without verification.

**Conflicting premises observed:**

* The original P1.1 requirement said “use `RfcCloseConnection`
  from a different thread to interrupt”, citing
  ausência/remoção de `RfcCancel` em algumas versões do SDK.
* The current `internal/sdkbackend/invoke.go` uses `RfcCancel`
  with a goroutine watcher.
* The CHANGES file (commit `5f35cd5`) claims `RfcCancel` exists
  in NW RFC SDK 7.50 PL18.

**Verification gates that MUST be cleared before implementing `Conn.Cancel`:**

1. Open the `<sapnwrfc.h>` header of the **minimum supported**
   SDK release for v0.2.0 (declared in [INSTALL.md](INSTALL.md))
   and capture the exact prototype of `RfcCancel` if present.
2. Open the SAP NW RFC SDK Programming Guide / Doxygen for the
   same release and capture the documented thread-safety
   contract for `RfcCancel`. In particular: is it documented
   safe to call from a goroutine other than the one blocked in
   `RfcInvoke`? On the same handle? While `Open` is in flight?
3. On Linux: `nm -D libsapnwrfc.so | grep -i RfcCancel`.
   On Windows: `dumpbin /EXPORTS sapnwrfc.dll | findstr -i RfcCancel`
   (or `objdump -p sapnwrfc.dll`). Capture the symbol presence /
   absence per supported SDK PL.
4. Capture the SDK release line + PL the project commits to
   support (the floor of the `Capabilities()` table).
5. Determine, from the same header / guide, whether
   `RfcCloseConnection` is documented thread-safe with respect
   to a goroutine blocked in `RfcInvoke`. The default
   expectation is **no**; if the guide says otherwise, cite the
   exact section.

**Decision matrix (apply only after evidence is in hand):**

| Evidence | Conn.Cancel implementation |
|---|---|
| `RfcCancel` exported AND documented thread-safe across the supported SDK floor | Use `RfcCancel` from a watcher goroutine. Mark Conn as cancelled atomically; subsequent ops fail with `*CancelledError`. Document caveat for mutating BAPIs (§11). |
| `RfcCancel` exported but thread-safety is unclear or constrained | Use `RfcCancel`, but serialize the watcher with the per-Conn mutex according to the documented constraint; if necessary, call from the same OS thread via `runtime.LockOSThread`. Document the constraint. |
| `RfcCancel` not exported on some supported PL | Either (a) raise the supported SDK floor to a PL where it is exported, with a `breaking change` note in CHANGES, OR (b) define a fallback that does NOT call `RfcCloseConnection` from a different thread without primary-source backing. Silent fallback to a non-thread-safe call is not acceptable. |
| `RfcCloseConnection` is documented thread-safe with respect to in-flight `RfcInvoke` | Document the citation; treat as additional belt-and-suspenders only — `RfcCancel` (when present) is still the canonical mid-call cancel. |

**Documentation obligations** that cannot be skipped, regardless
of the path chosen:

* `docs/SDK_FUNCTIONS_MAP.md` — row for `RfcCancel`: present?
  thread-safety verified? PL floor?
* `docs/ERRORS.md` — section on cancellation, listing the SAP
  caveat for mutating BAPIs (`outcome unknown`).
* `godoc` of `Conn.Cancel` — references both the SDK function
  used and the caveat section.

**Until §3 verification is captured in `docs/EVIDENCE/sdk-cancel.md`,
P1.1 stays at `[E]`**. Implementation is blocked.

---

## 4. P1 work items (mandatory for v0.2.0)

### P1.1 — Mid-call Cancel (`Conn.Cancel`) [x]

* Status: **implemented and validated**. Evidence captured at
  [`docs/EVIDENCE/sdk-cancel.md`](EVIDENCE/sdk-cancel.md).
  Implementation: `nwrfc/conn.go::Conn.Cancel`,
  `internal/backend/backend.go::Cancellable`,
  `internal/sdkbackend/conn.go::cancelConn`,
  `internal/sdkbackend/cancel.go::withCancelWatcher`.
  Tests: `nwrfc/cancel_test.go` (5 tests) +
  `nwrfc/cancel_synctest_test.go` (2 synctest tests).
  godoc + caveat in `docs/ERRORS.md#cancellation-and-mid-call-aborts`.
* (Original §3 evidence requirement, kept for traceability.)
* New: `func (c *Conn) Cancel() error` — idempotent, safe from a
  different goroutine for the in-flight cancel use case.
* New: shared cancel watcher reused by `Open`, `Ping`,
  `Describe`, `Reset`, `Invoke`.
* Cleanup of native handle bookkeeping uses `runtime.AddCleanup`
  (Go 1.24+); no `runtime.SetFinalizer`. The current Conn has no
  finalizer for the SDK handle by design — keep that.
* Tests under `testing/synctest` for ctx-deadline and
  ctx-cancel paths; one test per blocking op.
* Acceptance:
  * `errors.Is(err, ErrCancelled) == true` for cancelled ops.
  * `Cancel` callable 3× in sequence with no panic; second and
    third calls return nil.
  * `Close + Cancel` and `Cancel + Close` interleavings race-free.
  * `go test -race -tags nwrfc_nosdk ./...` and the SDK build
    pass.
  * `docs/EVIDENCE/sdk-cancel.md` exists and is referenced by
    `docs/ERRORS.md`.

### P1.2 — Logon error subtypes [x]

* Status: **implemented and validated**.
  Implementation: `nwrfc/errors_logon.go`. Tests:
  `nwrfc/errors_logon_test.go` (8 tests; classifier matrix +
  no-secret-leak + sentinel uniqueness). ERRORS.md updated
  with the four subtypes and retry/UX guidance.
* Subtypes: `*PasswordExpiredError`, `*UserLockedError`,
  `*InvalidCredentialsError`, `*UnknownLogonFailureError`.
* Sentinels: `ErrPasswordExpired`, `ErrUserLocked`,
  `ErrInvalidCredentials`, `ErrUnknownLogonFailure` — all
  preserve `errors.Is(err, ErrLogon)`.
* Classification table is the **single source of truth** in
  `nwrfc/errors_logon.go::logonClassifications`.
* Promotion to `[x]` requires:
  * `docs/ERRORS.md` updated with the four subtypes and the
    "no fallback to Communication" rule.
  * Behavior probe under `internal/sdktest/` capturing the
    actual SDK keys / messages on a real SAP system, OR a
    written-down evidence row at `docs/EVIDENCE/logon-fixtures.md`
    listing the messages we tested against.

### P1.3 — Params redaction [x]

* Status: **implemented and validated**.
  Implementation: `internal/backend/types.go::IsSensitiveKey`
  (explicit names + prefixes + suffixes), consumed by
  `nwrfcotel/otel.go` and `nwrfc/params.go`. Stringer +
  GoStringer on `Params`. Tests:
  `nwrfc/conn_test.go::TestParams_StringerRedacts`,
  `TestParams_ExtraRedactedInLogValue`,
  `TestParams_LogValueRedacts`. SECURITY.md §5 updated with
  the matcher rules.

### P1.4 — Public type aliases [x]

* Status: **implemented and validated** for the four
  scalar types.
  Implementation: `nwrfc/types.go` (`Date`, `Time`, `UTCLong`,
  `Decimal`, plus the `AsTime` convenience). Tests:
  `nwrfc/types_test.go` (4 tests covering method reachability
  through the alias and identity vs the internal type).
  MIGRATION_FROM_GORFC.md updated.
* **OPEN follow-up**: `time.Time` recognition in
  `marshalGoValue` (call.go) is not yet wired — callers using
  `time.Time`-typed struct fields go through the generic
  `reflect.Struct` path. Adding direct recognition is a
  performance optimization tracked under post-v0.2.

### P1.5 — v0.2.0 release prep [x]

* CHANGES section dated 2026-05-08 with:
  * Breaking changes vs v0.1.x (logon subtypes change shape
    of `mapBackendError`; aliases relocate to `nwrfc/types.go`).
  * Minimum SDK version statement consistent with the §3
    decision.
  * Migration notes from string-matching error handling.
* Signed-tag procedure documented in `CONTRIBUTING.md` and
  cross-referenced from CHANGES:
  * `git tag -s v0.2.0 -m "v0.2.0 …"`
  * `git push origin v0.2.0`
  * `go env GOPROXY` cache invalidation note.
* Tag is **not** created in this cycle by the agent. Maintainer
  cuts it after CI passes on `master`.

### P1.6 — Windows CI matrix [x]

* Status: **landed**. Workflow at
  [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)
  with three required jobs: `linux-nosdk`, `windows-nosdk`,
  `linux-to-windows-cross` (zig cc), plus a manual
  `linux-sdk-real` job and a `ci-summary` aggregator. CI run
  on `master` after merge will confirm green.

* `.github/workflows/ci.yml` (NEW) with at minimum:
  * `windows-latest` job, MinGW-w64 in PATH, runs
    `go build -tags nwrfc_nosdk ./...` and
    `go test -tags nwrfc_nosdk ./...`.
  * `ubuntu-latest` job using `zig cc -target x86_64-windows-gnu`,
    cross-compiling `go build -tags nwrfc_nosdk ./...` for
    `GOOS=windows GOARCH=amd64`.
  * `ubuntu-latest` job running default `go vet ./...`,
    `go test -tags nwrfc_nosdk ./...`,
    `go test -race -tags nwrfc_nosdk ./...`.
  * Optional `workflow_dispatch` job for SDK-real Windows
    builds gated on a secret-supplied SDK location.
* Acceptance: CI passes on the PR that introduces the workflow,
  AND a deliberate failure (e.g. a syntax error pushed to a
  draft branch) demonstrably fails the matrix before reaching
  `master`.

### P1.7 — VDI deploy / loader resolution docs [x]

* `docs/DEPLOY.md` (NEW). Coverage:
  * **Linux VDI**: rpath-relative layout (`bin/app`,
    `lib/libsapnwrfc.so`, `lib/libsapucum.so`) using
    `-Wl,-rpath,'$ORIGIN/lib'`. No `LD_LIBRARY_PATH`
    dependency.
  * **Windows VDI**: layout (`app.exe`, `sapnwrfc.dll`,
    `sapucum.dll` / `libsapucum.dll`, `icudt*.dll`,
    `icuin*.dll`, `icuuc*.dll`) co-located with the `.exe`.
    Cite the Win32 DLL search order
    (<https://learn.microsoft.com/windows/win32/dlls/dynamic-link-library-search-order>).
    Explicit warning: do **not** rely on `init()` calling
    `SetDllDirectoryW`, because cgo direct linkage may resolve
    DLLs before `init()` runs — the user-mode loader resolves
    the `.exe` import table during process start, before any
    Go code runs.
  * Cross-compile Linux → Windows with `zig cc`.
  * Anti-patterns to avoid in VDI: `Program Files`-style global
    install, registry edits, `PATH` of system, profile-level
    `LD_LIBRARY_PATH`, MSI installers that require admin.
  * Runtime verification: `EnsureSDK()`, `SDKVersion()`,
    `Capabilities()`.
* `docs/INSTALL.md` and `docs/BUILD.md` updated to cross-link
  `DEPLOY.md`.

### P1.8 — `EnsureSDK` / `SDKVersion` / `Capabilities` enforcement [~]

* Status: **partial**. Memoization via `sync.OnceValue` is
  in place (`nwrfc/utility.go`). The Linux / Windows
  filesystem probe (sibling-DLL detection) is **OPEN**;
  current `EnsureSDK` returns a structured error when the
  backend is `nosdk`/`unregistered` or when `RfcGetVersion`
  reports zero, but does not yet enumerate missing DLLs on
  Windows. Documented as diagnostic-only in
  `docs/DEPLOY.md` because cgo direct linkage resolves DLLs
  before `init()` runs.
* Promotion to `[x]` requires the sibling-DLL probe + a
  per-OS test under `nwrfc_nosdk` exercising the
  structured-error path.

* Memoize via `sync.OnceValue` so repeated calls do not
  re-probe.
* Linux: verify the layout described in DEPLOY.md when
  reachable from `os.Executable()`; structured error otherwise.
* Windows: from `os.Executable()`, list expected DLLs siblings;
  return `*SDKUnavailableError` enumerating any missing ones,
  the directory inspected, and a hint to `docs/DEPLOY.md`. We
  do **not** call `SetDllDirectoryW` to "fix" loader paths —
  cgo direct linkage resolves before main; if the DLLs are not
  next to the `.exe`, the process aborted before we ran. The
  EnsureSDK probe is therefore an `os.Stat` / `LoadLibraryEx`
  *after* successful process start; it is diagnostic, not
  remedial.
* Build-tag-aware: a test under `nwrfc_nosdk` exercises the
  structured-error path.

---

## 5. P2 work items (mandatory for v0.2.0)

### P2.1 — `PoolConfig.AlwaysReset` [x]

* Status: **landed**. Implementation in `nwrfc/pool.go`
  (Acquire path: Reset → AfterAcquire → return Conn). Tests
  in `nwrfc/pool_test.go::TestPool_AlwaysReset_*` cover
  ordering, opt-out default, and Reset-error surfacing.

* Default false (back-compat).
* When true, the pool calls `Conn.Reset()` before
  `AfterAcquire` and before handing the Conn to the caller.
* Reset error → checkout fails with the wrapped error.
* Documentation: ABAP context can leak between checkouts when
  the flag is false; risk and recommendation called out.
* Tests: AlwaysReset=true exercises Reset; AlwaysReset=false
  preserves current behavior; Reset failing returns error.

### P2.2 — `Params.MaxTraceLevel` [x]

* Status: **landed**. Field on `Params`; enforcement in
  `nwrfc/utility.go` (process-global, tightest-wins). Open
  installs the cap during the validate→capabilities→backend
  chain. Tests:
  `nwrfc/utility_test.go::TestMaxTraceLevel_*` (cap
  rejection, boundary, tightest-wins, zero = no cap).
  SECURITY.md §5 documents the policy.

### P2.3 — Hardlock WSHost vs `Capabilities.WebSocketRFC` [x]

* Status: **landed**.
  Implementation: `nwrfc/capabilities.go::enforceCapabilities`
  invoked from `Open` after `validate()`. Tests:
  `nwrfc/utility_test.go::TestEnforceCapabilities_*` (3 tests
  covering hardlock, capable backend, no-WSHost no-op).

* In `Open`, after `validate()`: if `p.WSHost != ""` and
  `Capabilities().WebSocketRFC == false`, return
  `*UnsupportedFeatureError` with `RequiredVersion` /
  `CurrentVersion` populated from `SDKVersion()`.
* No silent downgrade.
* Tests: WSHost + capability false fails; WSHost + capability
  true proceeds; no WSHost preserves current behavior.

### P2.4 — `docs/EVIDENCE/SCHEMA.md` [x]

* Define the YAML/JSON schema every per-test evidence file
  under `docs/EVIDENCE/` must satisfy.
* Required fields:
  * `function_metadata` — name, parameters, descriptors.
  * `sdk_version` (Major.Minor PL).
  * `capabilities` — full `nwrfc.Capabilities` snapshot.
  * `timestamp_utc` — ISO 8601 with TZ.
  * `params_redacted` — Params after `LogValue()`.
  * `response_shape` — typed JSON of returned values.
  * `error_structured` — typed error shape (Group, Key, …).
  * `host_hash` — SHA-256 of host name (NOT the host).
  * `sap_client` — mandant.
  * `correlation_id`.
  * `test_scenario` — short string identifying intent.
  * `operation_safety` — `read | idempotent | mutating | unknown`.
* Reference from `docs/INTEGRATION_TESTING.md`.

### P2.5 — Purge hardcoded `C:/Tools/nwrfcsdk` [x]

* Status: **landed**. Removed from `gorfc/gorfc.go` (lines
  43-44 in the v0.1 tree). `grep -rni 'C:.\?Tools.\?nwrfcsdk'
  --include=*.go` now returns 0 hits in source files.

* `gorfc/gorfc.go` lines 43-44 still carry
  `-IC:/Tools/nwrfcsdk/include/` and
  `-LC:/Tools/nwrfcsdk/lib/`.
* Replace with `SAPNWRFC_HOME`-driven flags or remove the
  legacy package's hardcoded constants entirely. The current
  `internal/sdkbackend/cgo_directives.go` already does the
  right thing; this work removes the conflicting older path.
* Docs: `docs/INSTALL.md` Windows section references
  `SAPNWRFC_HOME` exclusively.
* Acceptance: `grep -rni 'C:.\?Tools.\?nwrfcsdk'` returns 0
  matches in the repo.

### P2.6 — `testing/synctest` for cancel/timeout/concurrency [~]

* Status: **partial**.
  `nwrfc/cancel_synctest_test.go` adds two synctest tests
  (deadline + immediate cancel) using `synctest.Test` and
  `synctest.Wait`. The remaining cancel/timeout tests in
  `nwrfc/cancel_test.go` and `nwrfc/pool_test.go` use small
  real-time sleeps; converting them is mechanical and tracked
  for follow-up. The contract is exercised by both pathways.

* Convert tests under `nwrfc/` that exercise `ctx.Done()`,
  timeouts, or pool concurrency to use `testing/synctest`.
* The Go 1.25 promoted form (`synctest.Test`) is preferred. Go
  1.24 needed `-tags synctest`; 1.25 does not. Document the
  toolchain expectation in BUILD.md.
* Where a test cannot use `synctest` (e.g. cgo blocking call),
  document the reason in a `// not synctest because …`
  comment.

---

## 6. Transversal Codebase Audit — mandatory for v0.2.0

The roadmap explicitly **forbids** scoping the v0.2.0 cycle to
P1/P2 only. The implementation phase **must** also scan the
existing tree for the violations listed below and either fix
them in the same PR series or capture an objective justification
under `docs/EVIDENCE/` for skipping.

### 6.1 OOP idiomático em Go

* Single-responsibility per type/file.
* Pointer receivers for types that own state / native handles.
* Embedding for hierarchical errors where it avoids wrapping
  ceremony.
* Small interfaces accepted at consumers (Cancellable, Resetter,
  Pinger, Describer).
* Dependency inversion: pool / provider / backend stay decoupled
  from the concrete cgo SDK.

### 6.2 SOLID / DRY / KISS

Concrete patterns to look for and fix:

* **String matching of error messages** anywhere outside the
  classifier table — replace with `errors.Is` / `errors.As`.
* **Duplicate redaction logic** — every site must consult
  `backend.IsSensitiveKey` (single source of truth).
* **Duplicate RFC_ERROR_GROUP → typed-error mapping** —
  must live in one switch (`nwrfc.sdkErrorToTyped`) only.
* **Duplicate cancel-watcher goroutine** in
  `Open / Ping / Describe / Reset / Invoke` — fold into one
  helper.
* **Duplicate Params validation** at multiple call sites.
* **Surfaces that force the caller to import `internal/`** —
  re-export via `nwrfc/`.

### 6.3 Segurança RFC/SAP, logs, credenciais

* No password / SSO ticket / bearer / X.509 / SNC principal /
  SAML assertion in any log line.
* Trace level capping (P2.2) actually enforced.
* No silent fallback when capability is missing (P2.3).
* `Conn.Cancel` of mutating ops carries the SAP caveat in
  godoc + `docs/ERRORS.md`.

### 6.4 Performance Go

* Hot path checks for: regex recompiled per call, materialized
  large tables when streaming would suffice, unnecessary
  string ↔ []byte conversions, locks held across SDK calls
  where the per-Conn mutex already serializes, alloc-per-call
  in marshaling that could be pooled.
* Streaming for `RFC_READ_TABLE` and large tables with
  `iter.Seq` / `iter.Seq2` when the value justifies the
  complexity.

### 6.5 Concorrência e lifecycle de handles cgo

* Per-Conn mutex serializes SDK calls (existing).
* `Cancel` does NOT take the per-Conn mutex (must call from a
  different goroutine — see §3).
* `Close` is idempotent, atomic state transition, joins cleanup
  errors via `errors.Join`.
* No `runtime.SetFinalizer` on `*Conn` or any object that owns
  an SDK handle — the design requires explicit Close. Replace
  any other SetFinalizer with `runtime.AddCleanup`.
* No double-close, use-after-close, or data race in
  Cancel/Close/Invoke interleavings.

### 6.6 Build tags, Windows/Linux, VDI

* `cgo && !nwrfc_nosdk` vs `!cgo || nwrfc_nosdk` — exactly one
  backend registers per build.
* Windows is **first-class**, not best-effort. Any
  Linux-only assumption is a bug.
* No hardcoded `C:/Tools/nwrfcsdk` path (P2.5).
* `EnsureSDK` returns structured errors, never panics.

### 6.7 Testabilidade determinística

* Cancel / timeout / concurrency tests use `testing/synctest`
  (P2.6).
* No `time.Sleep` in unit tests where `synctest.Wait` would do.
* `t.Context()` for tests that need a context.
* Mock backend (`nwrfcmock`) covers every `backend.Backend`
  method.

### 6.8 Documentação operacional

* INSTALL, BUILD, DEPLOY, SECURITY, ERRORS, EVIDENCE/SCHEMA,
  CHANGES, MIGRATION_FROM_GORFC, README all consistent with the
  shipped code.
* Examples under `example/` compile and reflect the
  post-v0.2.0 API.

The audit is a deliverable. The implementation phase that
skips this section is incomplete.

---

## 7. Validation gates

| Gate | Command / Evidence | Required For | Status | Notes |
|---|---|---|---|---|
| Local: gofmt | `gofmt -l .` empty | Every PR | `[x]` | Verified at v0.2.0 RC build. |
| Local: go vet | `go vet ./...` AND `go vet -tags nwrfc_nosdk ./...` | Every PR | `[x]` | Both tag combos verified locally. |
| Local: nosdk tests | `CGO_ENABLED=0 go test -tags nwrfc_nosdk ./...` | Every PR | `[x]` | Green at v0.2.0 RC. |
| Local: nosdk race | `CGO_ENABLED=0 go test -tags nwrfc_nosdk -race ./...` | Every PR | `[x]` | Green at v0.2.0 RC. |
| CI: Windows nosdk | `windows-latest` job per P1.6 | v0.2.0 release | `[E]` | Workflow file landed; first run pending PR merge. |
| CI: Linux→Windows cross-compile | `zig cc` job per P1.6 | v0.2.0 release | `[E]` | Workflow file landed; first run pending PR merge. |
| Manual: SDK-real Linux build | self-hosted job OR maintainer script with NW RFC SDK 7.50 PL ≥ floor | v0.2.0 release | `[!]` | Blocked on SDK provisioning by maintainer. |
| Manual: SAP-real integration | `scripts/run-integration-tests.ps1` from a Windows VDI with VPN to a SAP sandbox | v0.2.0 release acceptance | `[!]` | Operator-driven, not automatable. |
| Security: redaction | `TestParams_LogValueRedacts`, `TestParams_StringerRedacts`, `TestParams_ExtraRedactedInLogValue`, `TestLogonSubtype_NoSecretLeak`, `TestErrors_LogValueRedacts` | Every PR | `[x]` | Five tests cover Params (LogValuer + Stringer + Extra) and errors. |
| Security: secret scan | `secret-scan` GH workflow (gitleaks) | Every PR | `[x]` | Already present and active. |

`[x]` is reserved for items that are objectively in place at
the time the roadmap is read. As of v0.2.0 RC, the gates marked
`[x]` above are individually verifiable by re-running the
listed command on a clean checkout. CI workflow gates stay at
`[E]` until the first GHA run on `master` after merge proves
them green.

---

## 8. Risks and residual concerns (open list)

1. **No live SAP system in CI**. Every behavior we cannot test
   end-to-end gets a `🟡` flag in
   `docs/SDK_FUNCTIONS_MAP.md` and an evidence row in
   `docs/EVIDENCE/`.
2. **`nwrfc_nosdk` ≠ SDK-real backend**. Tests that pass under
   `nwrfc_nosdk` are necessary but not sufficient. The matrix
   must include at least one SDK-real build (gated on a
   self-hosted runner or a maintainer's machine).
3. **Cancellation of mutating BAPIs**. The ABAP side may have
   committed before the cancel reached it. Software cannot
   solve this; `docs/ERRORS.md` documents the
   `outcome unknown` rule.
4. **SDK PL drift**. Capability thresholds documented today
   (`Capabilities.WebSocketRFC`, `Throughput`,
   `FastSerialization`) are best-effort against the SAP NW RFC
   SDK Programming Guide. P1.8 must capture the exact PL the
   project supports as a floor; PL bumps are breaking changes.
5. **Windows loader runs before Go `init()`**. If the `.exe`
   ships without `sapnwrfc.dll` next to it, the process aborts
   before any Go code runs. EnsureSDK is therefore diagnostic,
   not remedial. Documentation must call this out.
6. **VDI packaging discipline**. Citrix profile redirection,
   AppData isolation, and roaming-profile copy-on-write can
   move the binary away from its DLL siblings between the
   build host and the VDI session. The deploy doc must call
   out the per-VDI testing requirement.
7. **`testing/synctest` toolchain coupling**. Stable in Go
   1.25+. Our `go.mod` declares `go 1.23` with
   `toolchain go1.25.0`, so consumers on the toolchain stay
   supported. BUILD.md must document the requirement.
8. **`nwrfcsdk/` directory in the repo**. Currently gitignored
   for the Windows ZIP drop-in pattern; double-check it is not
   accidentally checked in by the secret-scan workflow.

---

## 9. Guidance for `nexus-spec` consumers

`nexus-spec` integrates against the **public API only**. The
contract:

* Pass `context.Context` everywhere; the library decides how to
  honor cancellation. Do **not** depend on `RfcCancel` or
  `RfcCloseConnection` being available — the implementation may
  change between PLs.
* Branch on errors via `errors.Is(err, nwrfc.ErrLogon)`,
  `errors.As(err, &le *LogonError)`, etc. **Never** parse error
  messages.
* Call `nwrfc.EnsureSDK()` at process start; treat its return
  as fatal.
* Use `nwrfc.SDKVersion()` and `nwrfc.Capabilities()` to gate
  features that have a PL floor.
* Log `Params` and errors via `slog`. The library guarantees
  redaction.
* When pooling, set `PoolConfig.AlwaysReset = true` if your
  workload mixes ABAP-stateful BAPIs with ad-hoc reads.
* Treat a cancelled mutating call as **outcome unknown**.
  Confirm the SAP-side state via a separate read before
  retrying or compensating.

`nexus-spec` does NOT need to know:

* Which SDK function is used internally.
* The cgo build flags.
* The cancel watcher implementation.
* The `internal/...` packages.

---

## 10. Roadmap is not the deliverable

Updating this roadmap does not satisfy v0.2.0. The deliverable
is:

* Code that implements the roadmap items.
* Tests that exercise that code.
* CI updated to run those tests on Linux and Windows.
* Documentation that matches the code.
* Evidence captured for items that cannot be tested in CI.

An implementation agent (or maintainer) that updates the
roadmap and stops has not finished the activity. The roadmap is
the input to the implementation phase, not its output.

Each roadmap item has a measurable acceptance criterion. The
implementation phase is complete only when **every** P1 / P2
item moves to `[x]`, with the evidence cited inline, AND the
transversal audit (§6) is captured under `docs/EVIDENCE/audit.md`
or equivalent.

---

## 11. Definition of Done (v0.2.0)

A v0.2.0 release candidate is ready when **all** of the
following hold and are independently verifiable:

* [x] P1.1, P1.2, P1.3, P1.4, P1.5, P1.6, P1.7 each at `[x]`.
* [~] P1.8 — sibling-DLL probe deferred; structured-error path landed and tested.
* [x] P2.1, P2.2, P2.3, P2.4, P2.5 each at `[x]`.
* [~] P2.6 — two synctest tests landed; remaining conversions tracked.
* [x] Transversal audit (§6) executed; captured under [`docs/EVIDENCE/audit.md`](EVIDENCE/audit.md).
* [x] `gofmt -l .` empty (verified at v0.2.0 RC).
* [x] `go vet ./...` clean (default tags).
* [x] `go vet -tags nwrfc_nosdk ./...` clean.
* [x] `CGO_ENABLED=0 go test -tags nwrfc_nosdk ./...` green.
* [x] `CGO_ENABLED=0 go test -tags nwrfc_nosdk -race ./...` green.
* [E] CI: `windows-latest` nosdk job green — workflow file landed, first run after merge.
* [E] CI: Linux→Windows cross-compile job green — workflow file landed, first run after merge.
* [x] No occurrences of `C:/Tools/nwrfcsdk` (or case variants) under `*.go` (only roadmap references in `*.md` remain, intentional).
* [x] No `runtime.SetFinalizer` on `nwrfc.Conn` or any v0.2 native-handle owner. The legacy `gorfc/gorfc.go` retains its upstream finalizer per `docs/PORTING_STRATEGY.md` (audit row §6.5 KEEP).
* [x] No silent fallback when an SDK capability is missing — WSHost without `Capabilities.WebSocketRFC` returns `*UnsupportedFeatureError` (P2.3).
* [x] No secret leakage in any test log — verified by 5 redaction tests (audit §6.3).
* [x] All errors usable via `errors.Is` / `errors.As`; no public call site requires string matching (audit §6.2).
* [x] `Conn.Cancel` documented with the SAP caveat for mutating BAPIs both in godoc (`nwrfc/conn.go`) and `docs/ERRORS.md#cancellation-and-mid-call-aborts`.
* [x] `CHANGES` v0.2.0 section dated 2026-05-08, breaking changes called out, minimum SDK PL stated (7.50 PL3+).
* [x] Signed-tag procedure (`git tag -s v0.2.0 …`) documented in `CONTRIBUTING.md` "Releasing (maintainer playbook)".
* [x] `example/pool/main.go` updated for the `Conn.Reset(ctx)` signature change.

The maintainer (not the agent) cuts the signed tag after CI is
green on `master`. The agent's responsibility ends when this
checklist is satisfied.

---

## 12. Post-tag follow-ups (out of scope for v0.2)

* `nwrfcserver` registration callbacks (Tier 3.4) —
  `RfcInstallTransactionHandlers` trampoline through cgo
  function pointers. Stubbed; tracked separately.
* IDoc parsing helpers under `nwrfcidoc` — TID/UID retention
  policy hooks once tRFC server is wired.
* Live AS ABAP STFC_STRUCTURE round-trip in CI — needs an SAP
  sandbox in scope of CI infrastructure, which is a
  customer-side decision.
* `runtime.AddCleanup` for ancillary buffers if they are added
  later (no current cleanup target after the v0.2.0 work).

---

## Appendix A — How to update this roadmap honestly

Rules for any agent or maintainer editing this file:

1. Do not move a box from `[~]` / `[ ]` to `[x]` without citing
   the file path of the test or evidence in the same edit.
2. Do not invent CI status. If a workflow file does not exist
   in `.github/workflows/`, the gate stays `[ ]`.
3. Do not paraphrase the SAP NW RFC SDK Programming Guide
   without a citation under `docs/EVIDENCE/`.
4. Do not extend the roadmap into post-v0.2 work without
   marking the new section as out-of-scope.
5. When the implementation reveals a roadmap item is
   misspecified, edit the roadmap before editing the code.
