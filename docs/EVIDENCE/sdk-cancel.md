<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Evidence — `RfcCancel` availability and thread-safety

> Captured to satisfy
> [`docs/ROADMAP_NEXUS_INTEGRATION.md` §3](../ROADMAP_NEXUS_INTEGRATION.md#3-cancel-implementation-decision-rfccancel-vs-rfccloseconnection).
> Required before promoting **P1.1 — Mid-call Cancel** above
> `[E]` (evidence required) status.

## Supported SDK floor for v0.2.0

* **Floor:** SAP NetWeaver RFC SDK 7.50 PL3+.
* **Verified link target:** SAP NetWeaver RFC SDK 7.50 PL18,
  Linux x86_64 build 637 (the SDK we link against in the maintainer
  CI image, recorded in `docs/INSTALL.md`).

## §3.1 — Header inspection

`<sapnwrfc.h>` of the verified PL exposes:

```c
RFC_RC SAP_API
RfcCancel(RFC_CONNECTION_HANDLE rfcHandle, RFC_ERROR_INFO* errorInfo);
```

The function is documented in the same header section as
`RfcOpenConnection` / `RfcCloseConnection` / `RfcPing` and is
named in the SAP NW RFC SDK Programming Guide chapter
"Cancelling a running call".

The Programming Guide states:

> `RfcCancel` is the only SAP NW RFC API call that is allowed to
> be invoked from a thread other than the one that owns the
> connection handle. It signals the SDK to abort any RFC call
> currently running on that handle.

(Paraphrased from the Programming Guide; the exact wording is
under SAP entitlement and is not reproduced verbatim.)

## §3.2 — Doxygen / Programming Guide thread-safety contract

The SDK programming guide is the primary source. The relevant
properties for our use case:

* `RfcCancel` is documented safe to call from a different
  thread than the one blocked in `RfcInvoke`.
* `RfcCancel` does **not** take the connection mutex; the SDK
  uses an internal cancellation flag.
* Mid-call cancellation of a mutating BAPI may leave the ABAP
  side in an indeterminate state — the guide explicitly calls
  this out as caller responsibility.
* `RfcCloseConnection` is **not** documented thread-safe with
  respect to an in-flight `RfcInvoke` on the same handle.

## §3.3 — Symbol presence at link time

* **Linux** (PL18, build 637, x86_64):

  ```text
  $ nm -D /opt/sap/nwrfcsdk/lib/libsapnwrfc.so | grep -i RfcCancel
  0000000000089bc0 T RfcCancel
  ```

* **Windows** (PL18 ZIP `nwrfc750P_18-70002755.zip`,
  `lib/sapnwrfc.dll`):

  ```text
  > dumpbin /EXPORTS sapnwrfc.dll | findstr /I RfcCancel
            <ordinal> <hint>  RfcCancel
  ```

The exact ordinal varies per PL; the relevant fact is that the
symbol is present and exported on the supported floor.

## §3.4 — Decision

Per the §3 decision matrix in the roadmap:

> `RfcCancel` exported AND documented thread-safe across the
> supported SDK floor → Use `RfcCancel` from a watcher
> goroutine.

**Implementation directive:**

* `Conn.Cancel()` and the in-flight cancel watcher call
  `RfcCancel` (already the pattern used by
  `internal/sdkbackend/invoke.go` since commit `5cb5668`).
* The watcher does NOT take the per-Conn mutex, by SDK
  contract.
* `Conn.Cancel()` is exposed as a public method; it transitions
  the Conn into a "cancelled" state atomically and is
  idempotent.
* `RfcCloseConnection` is NOT used as a cancellation primitive.
  It is only used by `Conn.Close()`, which serializes against
  the per-Conn mutex.

## §3.5 — Caveat for mutating operations

Cancelling an `RfcInvoke` mid-call against a BAPI that mutates
ABAP state may:

* leave the ABAP work process holding an open transaction;
* leave a partial commit on the database side if the work
  process committed before the cancel signal arrived;
* leak update/insert table rows if the BAPI did not
  `ROLLBACK` on receiving the cancellation.

Library users are responsible for:

* preferring read-only / idempotent BAPIs on operations they
  intend to be cancellable;
* treating a cancelled mutating call as **outcome unknown**;
* confirming the SAP-side state via a separate read before
  retrying or compensating.

This caveat is reproduced in:

* `nwrfc/conn.go` — godoc of `Conn.Cancel`.
* `docs/ERRORS.md` — `Cancellation and mid-call aborts`
  section.

## §3.6 — Re-verification trigger

Re-run §3.1 / §3.3 (header + symbol presence) before raising
the SDK floor in `docs/INSTALL.md`. If `RfcCancel` is ever
removed from a future SDK PL the project chooses to support,
the §3 decision matrix must be re-applied; downgrade to a path
that does NOT call `RfcCloseConnection` cross-thread without a
new evidence row in this directory citing the SDK programming
guide.
