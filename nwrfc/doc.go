// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package nwrfc is the modern Go connector for the SAP NetWeaver
// RFC SDK.
//
// It is the public API surface of the gorfc community revival:
// `Conn`, `Pool`, `Session`, the typed error hierarchy, and the
// struct-tag marshaling layer all live here. The cgo-bound SDK
// bindings live under `internal/sdkbackend`; this package never
// imports them directly, so importing `nwrfc` does not transitively
// pull in `import "C"`.
//
// # Status
//
// This package is the destination of the Tier 1 revival; see
// docs/PLAN.md §10 Tier 1 for the full deliverable list and
// acceptance criteria. Until Tier 1 lands fully, the public API
// is unstable and may change in any minor release. The symbols
// added incrementally per docs/PLAN.md §11 are kept compilable
// and tested at every step.
//
// # Build modes
//
// Default build (`CGO_ENABLED=1`, no `-tags nwrfc_nosdk`): the
// cgo backend is registered and operations dispatch to the SAP
// NetWeaver RFC SDK. Requires the SDK to be installed and
// reachable through CGO_CFLAGS / CGO_LDFLAGS or
// SAPNWRFC_HOME at build time.
//
// SDK-free build (`-tags nwrfc_nosdk` or `CGO_ENABLED=0`): the
// no-SDK stub backend is registered. Every operation returns an
// error wrapping [ErrSDKUnavailable]. Useful for CI environments
// that cannot install the SDK and for downstream packages that
// re-export `nwrfc` types but do not call into SAP themselves.
//
// # First call
//
// Once Tier 1.5 ships the real bindings, the canonical first
// call looks like:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	conn, err := nwrfc.Open(ctx, nwrfc.Params{
//	    AsHost: "sap.example.invalid",
//	    SysNr:  "00",
//	    Client: "100",
//	    User:   os.Getenv("SAP_USER"),
//	    Passwd: os.Getenv("SAP_PASS"),
//	    Lang:   "EN",
//	})
//	if err != nil { return err }
//	defer conn.Close()
//
//	type In struct {
//	    ReqText string `rfc:"REQUTEXT"`
//	}
//	type Out struct {
//	    EchoText string `rfc:"ECHOTEXT"`
//	    RespText string `rfc:"RESPTEXT"`
//	}
//	var out Out
//	if _, err := nwrfc.Call(ctx, conn, "STFC_CONNECTION", In{ReqText: "ping"}, &out); err != nil {
//	    return err
//	}
//
// See docs/PLAN.md §5 (Public API Proposal) for the exhaustive
// usage examples — pool, session, tRFC, qRFC, bgRFC, server,
// throughput, codegen, mock backend.
//
// # Lazy table streaming
//
// [Call] and [CallMap] materialize TABLES parameters before
// returning. For very large tables, use [CallTableStream] to
// keep the SDK function handle alive while rows are read one at
// a time:
//
//	res, err := nwrfc.CallTableStream(ctx, conn, "BAPI_MATERIAL_GETLIST", "MATNRLIST", in)
//	if err != nil { return err }
//	defer res.Close()
//
//	for {
//	    row, err := res.Next(ctx)
//	    if errors.Is(err, io.EOF) { break }
//	    if err != nil { return err }
//	    _ = row["MATERIAL"]
//	}
//
// While the stream is open the Conn is pinned and must not be
// returned to a Pool. Close is mandatory even after EOF or early
// break; it releases the function handle and unlocks the Conn.
//
// # Error handling
//
// Every error returned from this package implements
// [backend.Categorized] and matches one of the sentinels
// declared in `errors.go` via [errors.Is]. Concrete types can
// be extracted with [errors.As]:
//
//	if errors.Is(err, nwrfc.ErrLogon) { ... }
//
//	var abap *nwrfc.ABAPApplicationError
//	if errors.As(err, &abap) {
//	    fmt.Println(abap.AbapMsgClass, abap.AbapMsgNumber)
//	}
//
// All errors implement [slog.LogValuer] with redaction baked in.
// Logging an error via slog never emits secret material.
//
// # Security
//
// See docs/SECURITY.md for the credential-handling policy. The
// short version: this package never logs PASSWD, MYSAPSSO2,
// X509CERT, SNC_PARTNERNAME, or other sensitive material; the
// `slog.LogValuer` redaction layer enforces this even when
// callers pass `Params` or `*nwrfc.LogonError` to
// `slog.Info(...)` directly.
package nwrfc
