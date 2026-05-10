<!--
SPDX-FileCopyrightText: 2026 gorfc community contributors
SPDX-License-Identifier: Apache-2.0
-->

# SAP Gateway / RFC connectivity troubleshooting

This runbook supports operators triaging `RFC_COMMUNICATION_FAILURE` /
`partner ... not reached` (RC `750`) and adjacent connectivity failures
that the `nwrfc` CLI surfaces through `nwrfc.exe test-connection --json`.

The structured report attaches one stage label and one operator-targeted
recommendation per failure class, so the JSON can be pasted directly into
a Basis / network ticket without rewriting.

> Scope. This document covers the *outbound* RFC client connection issued
> by `nwrfc.Open`/`Conn.Ping`. The CLI is a client; `secinfo` /
> `reginfo` and registered `tp/program_id` semantics apply only to
> *registered server* programs (inbound to SAP) and are not exercised
> by `test-connection`. See the “Common confusion” section below.

## 1. Stage taxonomy

| `stage` | Meaning | First responder |
|---|---|---|
| `sdk_unavailable`            | The SAP NW RFC SDK could not be loaded (missing `sapnwrfc.dll`/`.so` or `SAPNWRFC_HOME`).                                              | VDI operator        |
| `params_missing`             | One of the required `GORFC_TEST_*` env vars is empty.                                                                                   | VDI operator        |
| `network_unreachable`        | TCP probe to the gateway port failed before the SDK was invoked (`net.Dial` refused / timed out).                                       | Network ops         |
| `gateway_communication_failure` | TCP reached the SAP gateway but the gateway rejected/reset the stream before authentication. Mapped from `*nwrfc.CommunicationError`. | SAP Basis + Network |
| `auth_failed`                | The gateway accepted the stream and routed to the dispatcher; SAP rejected the credentials. Mapped from `*nwrfc.LogonError`.            | SAP Basis           |
| `broken_connection`          | Mid-call connection break. Retry once; on persistence, capture trace.                                                                   | VDI operator        |
| `timeout`                    | Client deadline expired while waiting on SAP.                                                                                            | SAP Basis           |
| `ping_failed`                | Open + auth succeeded but `RFC_PING` failed (dispatcher / ICM state).                                                                    | SAP Basis           |
| `ok`                         | Connection healthy; no operator action required.                                                                                         | —                   |

`stage_init` only shows up if the report is inspected mid-flight; the
final report always overwrites it.

## 2. Diagnostic JSON contract

```json
{
  "ok": false,
  "connection": "failed",
  "stage": "gateway_communication_failure",
  "sdk_version": "7.50 PL18",
  "gateway_host": "vhilfws1wd01.sap.iconic.com.br",
  "gateway_service": "sapgw00",
  "gateway_port": 3300,
  "network_reachable": true,
  "connection_opened": false,
  "auth_reached": false,
  "sdk_error_key": "RFC_COMMUNICATION_FAILURE",
  "sdk_error_group": "COMMUNICATION_FAILURE",
  "rc": 750,
  "error": "nwrfc: communication failure: ... (host=... service=sapdp00 key=RFC_COMMUNICATION_FAILURE)",
  "recommendation": "SAP Basis + Network ops: TCP reached the SAP gateway but it rejected/reset before authentication; verify gw/acl_mode + gw/acl_info, SAProuter route, and source-IP allowlist on the iconic SAP edge"
}
```

### Field semantics

| Field | Source |
|---|---|
| `gateway_host` / `gateway_service` / `gateway_port` | derived from `Params` (direct: `AsHost` + `sapgw<NN>` / `3300+NN`; ws: `WSHost` + `WSPort`; load balanced: `MsHost` + `sapms<SID>`). When the SDK reports a different `service` (e.g. `sapdp00` for the dispatcher), the SDK value wins. |
| `network_reachable` | `net.DialTimeout(tcp, gateway_host:gateway_port, 4s)`. `null` when no port can be derived (e.g. message-server lookup). |
| `connection_opened` | `true` only after `nwrfc.Open` returns nil. |
| `auth_reached` | `true` once SAP processed credentials (success or `LogonError`). |
| `sdk_error_key` / `sdk_error_group` / `rc` | `RFC_ERROR_INFO.key` / canonical group label / `RFC_ERROR_INFO.code`. |
| `error` | Full SDK message run through `redactRuntimeSecrets`; never carries a configured password / SSO ticket / bearer / SAML2 / X509. |

## 3. RC 750 / `RFC_COMMUNICATION_FAILURE` — concrete decision tree

Follow in order. Stop at the first answer that matches.

1. **Is `network_reachable: false`?**
   - Yes → the iconic SAP edge / SAProuter / firewall rejected the
     three-way handshake. Skip to §4 “Network operations request.”
   - No → continue.

2. **Did `connection_opened: false` *and* `auth_reached: false`?**
   - Yes → TCP succeeded but the SAP gateway closed the stream
     before any logon byte. This is the canonical RC 750 case.
     Skip to §5 “SAP Basis request.”
   - No → continue.

3. **Did `auth_reached: true` and `connection_opened: false`?**
   - Yes → the dispatcher accepted the user but rejected the
     credentials. This is `auth_failed`, *not* a gateway issue.
     Coordinate with SAP Basis on user/client/password rotation;
     verify the user is not locked in `SU01`.
   - No → continue.

4. **Did `connection_opened: true` and `Ping` failed?**
   - Yes → `stage: ping_failed`. Open + auth succeeded; `RFC_PING`
     was rejected at runtime. Inspect SM21, dev_disp, and ICM
     state. Not a gateway issue.

5. **Did the operation hit `stage: timeout`?**
   - Increase the client-side timeout. If it remains, escalate to
     Basis to inspect work-process saturation.

## 4. Network operations request (`network_unreachable`)

Provide network ops with:

- VDI source IP (redact to `/32` if necessary). Captured by the VDI
  registration record in the `nexus-spec` operational ledger (see
  `internal/vdi/registry.go`); do NOT paste credentials.
- `gateway_host` and `gateway_service` from the JSON report.
- Output of `Test-NetConnection -ComputerName <gateway_host> -Port
  <gateway_port>` from inside the VDI session.
- The SAProuter string in use, if any.
- The SDK preflight JSON proving `dynamic_loading.ok: true`.
- A statement that no SAP authentication was attempted because the
  TCP probe never received a SYN+ACK.

Ask network ops to:

- Confirm allowlist of the VDI source CIDR for the SAP gateway port
  on the iconic SAP edge / Web Dispatcher / ZPA App Segment policy.
- Confirm the SAProuter route entry, if a SAProuter is on the path.
- Confirm no stateful firewall is rate-limiting / blackholing the
  source IP.
- Do **not** request a `0.0.0.0/0` allowlist or a blanket gateway
  permissive rule.

## 5. SAP Basis request (`gateway_communication_failure`)

Paste into the SAP Basis ticket (no secrets):

> The SAP NW RFC SDK on the VDI execution node (`nexus-spec /
> nexus-mcp` operational lane, GoClaw connector — see
> [`docs/NAMING_POLICY.md`](https://example.org/) below) successfully
> reaches the SAP gateway TCP socket, but the gateway closes the
> stream before the SDK transmits any authentication payload.
>
> - SDK version: `7.50 PL18`, `cgo_linked: true`, `dynamic_loading.ok: true`.
> - Target gateway host: `<gateway_host>`.
> - Target gateway service: `<gateway_service>` (`<gateway_port>`).
> - SDK error: `RFC_COMMUNICATION_FAILURE`, `rc=750`, group
>   `COMMUNICATION_FAILURE`.
> - `network_reachable: true`, `connection_opened: false`,
>   `auth_reached: false`.
> - Source: VDI operational node, source IP redacted on request;
>   the VDI hostname `OPENCLAWVDI` is the legacy Windows computer
>   name and is not the connector / product name.
> - The connector identifier *must* be **GoClaw** (Go-based RFC
>   bridge). Any prior `OpenClaw:nwrfc` label is incorrect product
>   naming and must be ignored when adding gateway allowlist
>   entries.

Ask SAP Basis (read-only diagnostics first; no destructive changes):

1. **Validate gateway reachability** with SMGW from a known-good
   internal source — confirm the gateway responds and is not
   rejecting unrelated connections.
2. **Inspect the gateway log** (`SMGW > Goto > Trace > Display File`
   or `dev_rd`) at the timestamp of the failed connection
   attempt; share the raw log line for the rejection (no caller
   secrets are ever in the log).
3. **Validate `gw/acl_mode` and `gw/acl_info`** on the SAP gateway.
   If active, confirm the VDI source IP (or the whole egress CIDR
   approved by network ops) is in the allowlist.
4. **Validate SAProuter** route table if the dial path goes through
   a SAProuter; make sure the gateway destination is reachable for
   the published source.
5. Only after the gateway permits the connection: validate
   `secinfo` / `reginfo` if a *registered server program* is the
   target use case (this is **not** the case for the current
   `nwrfc.exe test-connection`, which is an outbound client; see §6).
6. Defer user / client / authorization checks to a later pass —
   they are only reachable once the gateway accepts the stream.

Do **not** request:

- A wildcard `gw/acl_info` entry.
- An open `secinfo` / `reginfo` entry.
- Any change to PRD; the lane under test is DEV / Sandbox only.
- PFCG role mutation.

## 6. Common confusion — `secinfo` / `reginfo` vs RC 750 client failures

`secinfo` and `reginfo` constrain *registered server programs* (e.g.
`tp/saplsd`) that connect *into* the SAP gateway and announce a
`PROGRAM_ID`. When the failing call is an outbound RFC client (`Open
+ Ping`, the path used by `nwrfc.exe test-connection`), there is no
`PROGRAM_ID` to allowlist — the gateway either accepts the inbound
TCP socket or it rejects it.

If a prior triage report quoted a label like `OpenClaw:nwrfc` as a
“TP name” the SAP gateway rejected, that label originated as a
*caller-side trace tag* in the upstream report and was *not* a TP
name negotiated with `secinfo`. The correct path forward is the
gateway-ACL / SAProuter / firewall checks above.

If the deployment ever introduces a registered RFC server (using
`nwrfc.RegisterServer` with `ServerConfig.ProgramID`), the
`PROGRAM_ID` MUST be a stable, GoClaw-consistent identifier (for
example `GOCLAW_SAP_BRIDGE`); any allowlist request must reference
that identifier exactly. See [`docs/NAMING_POLICY.md`](#) in the
nexus-spec repository for the authoritative naming rule.

## 7. Reproduction commands (read-only, VDI-side)

```powershell
# 1. SDK and packaging health.
nwrfc.exe --version --json
nwrfc.exe health --json
nwrfc.exe preflight --json

# 2. Full structured diagnostic against the configured DEV/Sandbox.
nwrfc.exe test-connection --json
```

Always close any retained MCP / SAP session afterwards (the
`sap.session_close_all` MCP tool, or `Conn.Close` in code).

## 8. Secret handling

- `nwrfc.exe` never logs `GORFC_TEST_PASSWD` / `_PASSWORD` /
  `_MYSAPSSO2` / `_BEARER` / `_SAML2`. The redactor in
  `cmd/nwrfc/main.go::redactRuntimeSecrets` substitutes any
  configured value with `«redacted»` before the report is
  emitted.
- The CommunicationError/LogonError types implement
  `slog.LogValuer` with redacted ABAP message variables; the SDK
  message text is reproduced verbatim minus any matching
  configured secret value, so messages such as
  `"partner LB.iconic ... not reached"` flow through.
- The `network_reachable` probe runs on `gateway_host:gateway_port`
  *only*; it does not transmit any RFC payload, credential, or
  client / user data.
- Trace level >0 is gated by `Params.MaxTraceLevel`. Operators in
  regulated environments declare a non-zero ceiling at startup;
  see `nwrfc/params.go` and `docs/SECURITY.md`.

## 9. Boundary contract

- HTTP 200 from `/mcp` is *not* SAP proof. SAP proof requires
  inspecting `result.isError`, `structuredContent.ok`, and
  `content[].text` in the MCP JSON-RPC response.
- `REAL_MCP` remains off by default. PRD, PFCG mutations, and
  standard-object mutations remain blocked at the MCP layer.
- Mock and simulator backends are unaffected by gateway-level
  failures and remain functional regardless of the diagnostic
  stages above.
