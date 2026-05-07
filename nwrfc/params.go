// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"log/slog"
	"strings"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Params describes a SAP RFC connection. It is the public,
// strongly-typed surface that callers populate; the wrapper
// converts to [backend.Params] before reaching the SDK.
//
// Three transport shapes are supported (use exactly one):
//
//  1. **Direct** — populate AsHost + SysNr (CPIC transport over
//     port 33xx + system number).
//  2. **Load-balanced** — populate MsHost + R3Name + Group
//     (transport via the SAP message server).
//  3. **WebSocket RFC** — populate WSHost + WSPort (TLS
//     recommended via TLSClientPSE / SNC). Capability-gated
//     against SDK ≥ 7.50 PL10 — see Tier 1.12 for the runtime
//     check.
//
// Logon credentials follow the same shape as the SAP NWRFC SDK
// parameters; the field names are intentionally close to the
// SDK's parameter strings so the mapping is grep-friendly.
//
// Extra carries SDK parameters not modeled as fields (a future
// SDK release may add new params; users should not have to wait
// for a library bump). Keys must use the lowercase SDK form
// (e.g. "trace_dir", "saml2", "snc_qop").
//
// Params implements [slog.LogValuer] with redaction over
// backend.SensitiveKeys, so logging a Params via slog.Info
// never leaks credentials.
type Params struct {
	// Destination name from sapnwrfc.ini (mutually exclusive
	// with the host fields below; use OpenDest if you go via
	// destination).
	Dest string

	// Direct connection (sRFC over CPIC). Populate AsHost +
	// SysNr.
	AsHost string
	SysNr  string

	// Load-balanced connection (via message server). Populate
	// MsHost + R3Name + Group.
	MsHost string
	R3Name string // SAP system ID (3-letter, e.g. "PRD")
	Group  string // logon group ("PUBLIC" by default)

	// WebSocket RFC. Populate WSHost + WSPort; TLS via
	// TLSClientPSE / SNC fields.
	WSHost string
	WSPort string

	// Logon parameters.
	Client string
	User   string
	Passwd string
	Lang   string

	// MYSAPSSO2 ticket (mutually exclusive with Passwd).
	Mysapsso2 string

	// X.509 certificate logon. The SDK accepts the cert in
	// PEM form. Mutually exclusive with Passwd / Mysapsso2.
	X509Cert string

	// SAML 2.0 / Bearer token. Capability-gated against SDK
	// ≥ 7.50 PL12 (🟡 verify). Mutually exclusive with the
	// other auth fields.
	SAML2  string
	Bearer string

	// SNC parameters. SncMyName / SncPartnerName carry SNC
	// principal names; redacted in logs because they often
	// reveal corporate domain structure.
	SncQOP         string // 1=integrity, 2=privacy, 3=max (default for production)
	SncLib         string // path to libsapcrypto / sapcrypto.dll
	SncMyName      string
	SncPartnerName string
	SncSso         string // "0" / "1"

	// TLS for WebSocket RFC.
	TLSClientPSE string
	TLSTrustAll  string // "0" / "1"

	// Operational toggles.
	Trace string // "0" .. "3"

	// Extra carries SDK parameters not modeled by named
	// fields. Keys must be lowercase SDK names; values are
	// passed through verbatim.
	Extra map[string]string
}

// LogValue redacts sensitive fields per
// backend.SensitiveKeys. Operators can log a Params via
// slog.Info without leaking credentials:
//
//	slog.Info("opening", "params", p)
func (p Params) LogValue() slog.Value {
	return p.toBackendParams().LogValue()
}

// toBackendParams converts the public Params struct into the
// internal backend.Params map shape. Empty fields are omitted
// so the SDK only sees keys the caller set.
//
// Field-to-key mapping uses lowercase SDK names matching the
// programming guide.
func (p Params) toBackendParams() backend.Params {
	out := make(backend.Params)
	put := func(key, val string) {
		if val == "" {
			return
		}
		out[key] = val
	}
	put("dest", p.Dest)
	put("ashost", p.AsHost)
	put("sysnr", p.SysNr)
	put("mshost", p.MsHost)
	put("r3name", p.R3Name)
	put("group", p.Group)
	put("wshost", p.WSHost)
	put("wsport", p.WSPort)
	put("client", p.Client)
	put("user", p.User)
	put("passwd", p.Passwd)
	put("lang", p.Lang)
	put("mysapsso2", p.Mysapsso2)
	put("x509cert", p.X509Cert)
	put("saml2", p.SAML2)
	put("bearer", p.Bearer)
	put("snc_qop", p.SncQOP)
	put("snc_lib", p.SncLib)
	put("snc_myname", p.SncMyName)
	put("snc_partnername", p.SncPartnerName)
	put("snc_sso", p.SncSso)
	put("tls_client_pse", p.TLSClientPSE)
	put("tls_trust_all", p.TLSTrustAll)
	put("trace", p.Trace)
	for k, v := range p.Extra {
		// Defensive: never let Extra override a named field
		// that was explicitly set; if a caller passes the
		// same key both ways, the named field wins.
		k = strings.ToLower(k)
		if _, exists := out[k]; exists {
			continue
		}
		put(k, v)
	}
	return out
}

// validate runs lightweight pre-flight checks before reaching
// the SDK. Returns *ConfigError on the first violation.
func (p Params) validate() error {
	// Mutually-exclusive transport: at least one of (Dest,
	// AsHost, MsHost, WSHost) must be set.
	if p.Dest == "" && p.AsHost == "" && p.MsHost == "" && p.WSHost == "" {
		return &ConfigError{
			Field: "transport",
			Hint:  "set one of Dest, AsHost+SysNr, MsHost+R3Name+Group, or WSHost+WSPort",
		}
	}
	// Direct without sysnr is a logon-time error in the SDK,
	// but we surface it earlier with a cleaner message.
	if p.AsHost != "" && p.SysNr == "" {
		return &ConfigError{Field: "SysNr", Hint: "required when AsHost is set"}
	}
	if p.MsHost != "" && (p.R3Name == "" || p.Group == "") {
		return &ConfigError{Field: "R3Name+Group", Hint: "required when MsHost is set"}
	}
	if p.WSHost != "" && p.WSPort == "" {
		return &ConfigError{Field: "WSPort", Hint: "required when WSHost is set"}
	}
	// Auth: zero-or-one of password/sso/x509/saml/bearer
	// (the SDK accepts password+SNC, password+TLS).
	authCount := 0
	if p.Passwd != "" {
		authCount++
	}
	if p.Mysapsso2 != "" {
		authCount++
	}
	if p.X509Cert != "" {
		authCount++
	}
	if p.SAML2 != "" {
		authCount++
	}
	if p.Bearer != "" {
		authCount++
	}
	if authCount > 1 {
		return &ConfigError{
			Field: "auth",
			Hint:  "set at most one of Passwd, Mysapsso2, X509Cert, SAML2, Bearer",
		}
	}
	return nil
}
