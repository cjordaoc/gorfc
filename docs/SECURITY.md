<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Security Policy (v0 — Tier 0)

> Status: **v0**, scoped to Tier 0 of [docs/PLAN.md](PLAN.md). Sections
> covering server runtime, OpenTelemetry redaction, and crypto loader
> behavior land in later tiers and are stubbed here. The full security
> model is specified in [PLAN.md §8](PLAN.md#8-security-model).

This document defines the **non-negotiable** security rules for the
revived `gorfc` project. It governs what may be committed, what may be
logged, and what users must obtain themselves under their own SAP
entitlement.

It is the operational counterpart to [AGENTS.md](../AGENTS.md) — AGENTS
defines the rules for AI assistants working in this repo; this file
defines the rules for the codebase and its operators.

## 1. Reporting a vulnerability

If you believe you have found a security issue in this repository, **do
not open a public issue**. Send the report to the maintainers via
encrypted email with subject `[gorfc-security]` and a private fork link
or patch attached. Public disclosure timelines follow [responsible
disclosure norms](https://www.first.org/global/sigs/vrdx/) — typically
90 days from acknowledgement, sooner if the fix lands first.

For SAP-side vulnerabilities (the SDK, ABAP servers, transports), report
through SAP Support or your SAP security team. This project does not
ship SAP code and cannot triage SAP issues.

## 2. Never commit

The following artifacts MUST NEVER be committed, in any branch, in any
form, including in tests, examples, fixtures, comments, screenshots,
generated files, or rebased history:

- **Real SAP credentials**: passwords, MYSAPSSO2 tickets, X.509
  certificates with private keys, SNC keys, SAP service account tokens.
- **Production `sapnwrfc.ini`** files. Use `sapnwrfc.ini.example` with
  placeholder values only. Real files are blocked by `.gitignore`.
- **PSE files** (`*.pse`) and SAP secudir contents.
- **SAP NetWeaver RFC SDK binaries**, headers, DLLs, shared libraries,
  `libsapnwrfc*`, `libsapucum*`, `librfccm*`, the `nwrfcsdk/` directory,
  or any artifact from the SAP Software Center.
- **SAP CommonCryptoLib / sapcrypto** binaries (`libsapcrypto.so`,
  `sapcrypto.dll`, `sapgenpse`, ...).
- **Customer-specific SAP system identifiers**: real ASHOST/MSHOST
  hostnames, real SID/system numbers tied to an identifiable customer,
  real client numbers tied to production. Use `*.example.invalid`
  hostnames in docs and examples.
- **Trace files** (`*.trc`) and RFC log files (`*.log`) — they may
  contain payloads with business-sensitive data.

The `.gitignore` enforces most of these patterns. CI secret scanning
(see §6) is the second line of defense.

## 3. Credential handling at runtime

Operators MUST:

1. Provide credentials through one of these channels:
   - environment variables resolved at process start;
   - a secrets manager (HashiCorp Vault, AWS Secrets Manager, Kubernetes
     Secrets, Azure Key Vault) injected as env or mounted file;
   - a `sapnwrfc.ini` file with `chmod 0600` ownership and least
     privilege;
   - SNC, X.509, MYSAPSSO2, or SAML/Bearer where the SDK supports it
     (Tier 2 deliverable — see [PLAN.md §10](PLAN.md#tier-2--observabilidade-providers-throughput-server-síncrono-69-pm)).
2. Prefer SNC/X.509/SSO over password auth in production.
3. Rotate credentials according to corporate policy and SAP password
   policy (`SU01` profile).
4. Restrict file system access to the runtime user only.

Operators MUST NOT:

- Pass credentials on the command line (visible via `ps`).
- Bake credentials into container images or Helm values committed to
  any repository.
- Transmit credentials in cleartext over channels other than the SAP
  RFC connection itself (which is encrypted only when SNC or
  WebSocket-RFC TLS is configured).

## 4. Logging, tracing, and observability

The library MUST NOT log:

- `PASSWD`, `PASSWORD`, or any password-shaped value.
- `MYSAPSSO2` tickets.
- `X509CERT` content.
- SNC partner names tied to customer identity (`SNC_MYNAME`,
  `SNC_PARTNERNAME`).
- `IDOC_INBOUND_ASYNCHRONOUS` and similar payloads at default verbosity
  (Tier 1+ deliverable).
- `ABAPApplicationError` payloads that may contain business data
  (use the typed error fields documented in
  [PLAN.md §7](PLAN.md#7-error-taxonomy); never `fmt.Sprintf("%v", err)`
  the whole struct at INFO).

Redaction is enforced at three layers:

1. **At source**: parameter and field names listed in §4 above are
   replaced with `«redacted»` before reaching `slog`, OpenTelemetry, or
   `fmt.Print*`. Implementation: `slog.LogValuer` on credential and
   error types — Tier 1 deliverable.
2. **At handler**: a `RedactHandler` wrapping any `slog.Handler` strips
   sensitive attributes by name, even when callers forget to use the
   typed shapes — Tier 2 deliverable.
3. **At egress**: span attributes in `nwrfcotel/` are filtered before
   being added to a span; payload-level data is opt-in only, never on by
   default — Tier 2 deliverable.

Until Tiers 1–2 land, the legacy `gorfc/` code does NOT enforce
redaction automatically. Operators using the legacy package MUST
configure their logging stack to filter the field names listed above.

## 5. Trace control

The SAP NetWeaver RFC SDK supports per-connection RFC tracing via
`RfcSetTraceLevel` and the `TRACE` ini parameter. Trace level >= 2
captures payloads. The library MUST:

- Default trace level to 0 (off) for new connections.
- Document the security impact of higher levels (Tier 1 deliverable in
  `docs/CONFIGURATION.md`).
- Never enable trace level > 0 in CI or examples without scrubbing
  output before commit.

### v0.2.0 — `Params.MaxTraceLevel` cap

`nwrfc.Params` exposes a `MaxTraceLevel int` field that caps
the maximum SDK trace level the process may set. The cap is
declarative and process-global with **tightest-wins**
semantics:

- Zero (the default) means "no cap".
- Setting `Params{MaxTraceLevel: N}` and calling `Open` with
  `N > 0` installs `N` as the floor. Any later `Params{
  MaxTraceLevel: M}` with `M < N` further tightens. A value
  with `M > N` is silently ignored — the cap only ever moves
  down.
- `nwrfc.SetTraceLevel(n)` with `n > MaxTraceLevel` returns
  `*ConfigError` referencing this section.
- The cap is process-global because the SDK trace state is
  itself process-global (`RfcSetTraceLevel(NULL, NULL, ...)`).
  Per-Conn caps would race; we make the constraint
  monotonically restrictive.

In regulated environments set `MaxTraceLevel = 1` (or `0`) at
process start to prevent any downstream library code from
raising trace verbosity beyond what your risk assessment
allowed:

```go
_, err := nwrfc.Open(ctx, nwrfc.Params{
    AsHost: ..., User: ..., Passwd: ...,
    MaxTraceLevel: 1, // cap: never above level 1
})
```

The cap is the security gate, not a hint. Implementation
reference: [`nwrfc/utility.go`](../nwrfc/utility.go).

### Redaction matcher (v0.2.0+)

The single source of truth for "is this Params key sensitive"
is `internal/backend.IsSensitiveKey`. It matches three ways
(case-insensitive after trim):

* **Explicit names**: `passwd`, `password`, `mysapsso2`,
  `x509cert`, `snc_myname`, `snc_partnername`, `snc_sso`,
  `tls_client_pse`, `tls_trust_all`, `saml2`, `bearer`.
* **Prefixes**: `passwd*`, `password*`, `secret*`, `bearer*`,
  `saml*`, `x509*`, `snc*`, `tls_*`, `mysapsso*`.
* **Suffixes**: `*_token`, `*_secret`, `*_key`,
  `*_credential`, `*_credentials`, `*_password`, `*_passwd`.

`Params.Extra[k]` entries are run through the same matcher,
so future SAP additions like `client_secret`, `api_token`,
or hypothetical `snc_qos_token` are redacted by default —
operators do not have to wait for a library bump.

`Params` implements `slog.LogValuer`, `fmt.Stringer`, AND
`fmt.GoStringer`, so accidental `fmt.Println(p)` /
`fmt.Sprintf("%+v", p)` cannot leak credentials.

## 6. CI secret scanning

The repository runs **gitleaks** on every push and pull request via
`.github/workflows/secret-scan.yml`. The workflow:

- Fails the build on any detected secret matching gitleaks default
  rules.
- Cannot be bypassed by force-push (the workflow runs on `push` to all
  branches including `master`).
- Will be extended in Tier 1 with a custom ruleset for SAP-specific
  patterns (`PASSWD=...`, `MYSAPSSO2=...`, `SNC_MYNAME=p:CN=...` with a
  real corporate domain, `*.wdf.sap.corp` hostnames).

Local protection: contributors are encouraged to install
[`pre-commit`](https://pre-commit.com/) and the gitleaks hook locally.

## 7. License boundary

The SAP NetWeaver RFC SDK, SAP CommonCryptoLib, and sapcrypto are
proprietary SAP products distributed under SAP licenses. They are NOT
redistributed by this project. Users obtain them from the SAP Software
Center under their own SAP entitlement.

This repository is Apache-2.0. Linking against the SAP SDK is permitted
under the terms of that license; redistributing SAP binaries is not.

## 8. Historical exposure

The upstream `SAP-archive/gorfc` repository historically committed a
`gorfc/sapnwrfc.ini` file containing real-looking credentials and
hostnames in the `wdf.sap.corp` domain. That file has been removed
from the working tree at the start of the revival but **remains
visible in the git history of this fork**.

Mitigation:

- This file describes the exposure publicly so operators know to rotate
  any credentials that might still be valid.
- Users who treat git history as untrusted should clone with
  `--shallow-since=<revival-date>` or filter history before mirroring.
- A future history rewrite (BFG / `git filter-repo`) is under
  evaluation; it is not done by default because rewriting public
  history breaks downstream forks and tags.

If you operated a SAP system at any of the historically-exposed
hostnames, treat the credentials as compromised regardless of the time
elapsed.

## 9. Versioning of this policy

| Version | Tier | Scope |
|---|---|---|
| **v0** (this) | T0 | Credentials, ini files, gitignore, SDK redistribution, CI secret scan, historical exposure note. |
| v1 | T1 | Adds typed-error redaction, `slog.LogValuer` on Conn/Params, default trace level policy enforced in code. |
| v2 | T2 | Adds `RedactHandler`, OpenTelemetry attribute filter, Server runtime threat model, SNC/X.509/SAML guidance. |
| v3 | T3 | Adds tRFC/qRFC/bgRFC and inbound server threat model. |

Changes to this file land in the same PR as the corresponding code
changes; the policy and the implementation move together.
