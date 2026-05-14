<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Evidence Schema

> Audience: SREs, integration testers, AI agents writing
> Categoria-B (live SAP) test reports.

Tests that exercise the `nwrfc` library against a live SAP
system cannot run in public CI (we do not redistribute SDK
binaries or expose SAP credentials). Operators run them
manually via [`scripts/run-integration-tests.ps1`](../scripts/run-integration-tests.ps1)
on a Windows VDI with VPN access to a SAP sandbox.

This document defines the **mandatory schema** for the
artifacts those manual runs produce. Every evidence file under
[`docs/EVIDENCE/`](.) that documents a SAP-real outcome must
satisfy this schema. No free-form prose-only files.

The schema is **YAML 1.2** (or JSON; YAML rendered to JSON
must satisfy the same shape). Filename pattern:

```
docs/EVIDENCE/<scenario>-<YYYYMMDD-HHmm>Z.yaml
```

## Canonical example

```yaml
# Scenario: STFC_CONNECTION echo on the EU-1 sandbox.
# Operator: ops-csm@example.invalid
# Run via: scripts/run-integration-tests.ps1 (smoke mode)
schema_version: 1
test_scenario: stfc_connection_echo

# When the run executed.
timestamp_utc: "2026-05-08T14:30:12Z"

# Library + SDK identity.
library:
  module: github.com/cjordaoc/gorfc
  version: v0.2.0-rc1
  commit: 7b2f1c0e
sdk_version: "7.50 PL18"

# Active capabilities at run time. Captured verbatim from
# nwrfc.Capabilities() — the goal is to enable diff-versus-PL.
capabilities:
  WebSocketRFC: true
  Throughput: false
  BgRFC: true
  UTCLong: true
  FastSerialization: true

# Function metadata: name + parameter shapes the test
# exercised. Captured from nwrfc.Conn.Describe.
function_metadata:
  name: STFC_CONNECTION
  parameters:
    - name: REQUTEXT
      type: RFCTYPE_CHAR
      direction: IMPORT
      length: 255
    - name: ECHOTEXT
      type: RFCTYPE_CHAR
      direction: EXPORT
      length: 255
    - name: RESPTEXT
      type: RFCTYPE_CHAR
      direction: EXPORT
      length: 255

# Connection params after redaction. Equivalent to slog.Info
# the Params; never include passwd/sso/x509/saml/snc material.
params_redacted:
  ashost: «redacted-by-policy»  # captured value redacted via host_hash below
  sysnr: "00"
  client: "100"
  user: ops-rfc
  passwd: «redacted»
  lang: EN

# Host identity by hash, never the host plain. SHA-256 of the
# DNS name (or IP if direct), no salt — the goal is recognizable
# stable identity, not authentication.
host_hash: "sha256:7d3b…f4a1"

# SAP client (mandant) the test ran against.
sap_client: "100"

# Correlation id propagated through the operator's logging
# stack (Datadog, ELK, …). Useful when correlating an evidence
# file with downstream alerts.
correlation_id: "ops-2026-05-08-EU1-014"

# Operation safety classification — drives later review of the
# run. `read` and `idempotent` are safe to retry; `mutating`
# documents that a cancellation could leave the SAP side in
# an indeterminate state.
operation_safety: read

# Response shape captured from the typed Out struct. The values
# are the actual round-tripped payload; redact business data
# before committing.
response_shape:
  ECHOTEXT: "ping"
  RESPTEXT: "Hi from SAP"

# Structured error if the run produced one. nil for happy-path
# scenarios. Mirrors the SDKErrorInfo + Category fields from
# the public errors.go types.
error_structured: null
# Example for a logon-failure scenario:
# error_structured:
#   category: logon
#   subtype: invalid_credentials
#   group: 3
#   key: RFC_LOGON_FAILURE
#   code: 99
#   message: «redacted»
#   abap_msg_class: ""
#   abap_msg_type: ""
#   abap_msg_number: ""

# Free-form notes — keep brief; one sentence per fact.
notes:
  - "First post-PL18 verification of STFC_CONNECTION echo."
  - "Run did not exercise UTCLong; tracked separately."
```

## Required fields

Every evidence file must populate these keys. A missing
required field invalidates the file.

| Field | Type | Notes |
|---|---|---|
| `schema_version` | int | Currently `1`. Bump when this document changes shape. |
| `test_scenario` | string | Stable identifier; safe in filenames. |
| `timestamp_utc` | RFC 3339 / ISO 8601 | UTC, with `Z` suffix. |
| `library.module` | string | Always `github.com/cjordaoc/gorfc`. |
| `library.version` | string | The library tag or `vX.Y.Z-rcN`. |
| `library.commit` | string | Short git SHA (>= 7 chars). |
| `sdk_version` | string | "Major.Minor PL<n>" form. |
| `capabilities` | object | Verbatim `nwrfc.Capabilities()` snapshot. |
| `function_metadata.name` | string | RFM name. |
| `function_metadata.parameters[]` | array | Per-param `name`, `type`, `direction`, `length`. |
| `params_redacted` | object | `nwrfc.Params` after `LogValue()`. |
| `host_hash` | string | `sha256:<hex>`. NOT the host plain. |
| `sap_client` | string | Mandant. |
| `correlation_id` | string | Operator-controlled trace id. |
| `operation_safety` | enum | `read \| idempotent \| mutating \| unknown`. |
| `response_shape` | object | The typed Out values, with business data redacted. |
| `error_structured` | object \| null | Structured form of any returned error. |

## Optional fields

| Field | When to use |
|---|---|
| `notes` | Free-form list of one-sentence observations. |
| `attachments[]` | Paths to additional artifacts (trace files, screenshots) under the same directory. |
| `operator_id` | When the operator wants attribution in the file (alternative: rely on git author). |
| `cancellation_summary` | For tests that exercise mid-call cancel: outcome, suspected SAP-side state, follow-up. |

## Validation

A simple `yq`-based validator lives in
[`scripts/validate-evidence.sh`](../../scripts/validate-evidence.sh)
(planned, not in v0.2.0). Until then, reviewers verify the
schema by hand against this document.

## Why a schema?

* **Reproducibility.** A future engineer reading the evidence
  needs to know the SDK PL, the library version, and the exact
  function metadata that was exercised. Free-form prose loses
  these.
* **Comparison across runs.** When PL or capabilities drift,
  diffing two evidence files highlights the change.
* **Security review.** A schema makes redaction enforceable:
  reviewers grep for `passwd`, `bearer`, `mysapsso2`, `x509`,
  `snc`, `saml` in evidence files; any hit in non-redacted
  positions is a security finding.
* **Traceability.** `correlation_id` ties the evidence file
  back to the operator's observability stack.
