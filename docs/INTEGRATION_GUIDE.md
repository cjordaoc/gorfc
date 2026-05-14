<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Integration Guide

This guide is the consumer contract for services that are replacing
SOAP `/sap/bc/soap/rfc` calls with native RFC calls through `gorfc`.
It is written for the `nexus-spec` migration, but the patterns apply to
any Go service that owns its SAP configuration and call lifecycle.

## Build Prerequisites

Production builds use the SAP NetWeaver RFC SDK through cgo:

```bash
export CGO_ENABLED=1
export SAPNWRFC_HOME=/opt/sap/nwrfcsdk
export CGO_CFLAGS="-I${SAPNWRFC_HOME}/include"
export CGO_LDFLAGS="-L${SAPNWRFC_HOME}/lib -Wl,-rpath,${SAPNWRFC_HOME}/lib"
go test ./...
```

The SDK, CommonCryptoLib, `sapcrypto`, and customer SAP credentials are
not part of this repository and must never be committed or redistributed.
For CI jobs that only compile or run SDK-free tests, use the no-SDK
backend:

```bash
go test -tags nwrfc_nosdk ./...
```

`-tags nwrfc_nosdk` compiles the public API and fails explicitly at
runtime with `*nwrfc.SDKUnavailableError` unless a test installs
`nwrfcmock`.

## Connection Pattern

Use `nwrfc.Open` for isolated or low-frequency calls where the caller can
close the connection immediately:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

conn, err := nwrfc.Open(ctx, nwrfc.Params{
	AsHost: cfg.Host,
	SysNr:  cfg.SystemNumber,
	Client: cfg.Client,
	User:   cfg.User,
	Passwd: cfg.Password,
	Lang:   cfg.Language,
})
if err != nil {
	return err
}
defer conn.Close()
```

Use `nwrfc.Pool` for repeated BAPI traffic. The pool owns connection
reuse; the caller owns only the work function passed to `Do`.

```go
pool, err := nwrfc.NewPool(nwrfc.PoolConfig{
	Params:         params,
	MinSize:        2,
	MaxSize:        16,
	IdleTimeout:    5 * time.Minute,
	MaxLifetime:    30 * time.Minute,
	AcquireTimeout: 5 * time.Second,
	AfterAcquire: func(ctx context.Context, c *nwrfc.Conn) error {
		return c.Reset()
	},
})
if err != nil {
	return err
}
defer pool.Close()

err = pool.Do(ctx, func(c *nwrfc.Conn) error {
	_, err := nwrfc.Call(ctx, c, "RFC_PING", nil, nil)
	return err
})
```

A single `Conn` serializes in-flight SDK work. Use a pool for
parallelism.

## Call Pattern

Prefer typed structs for BAPIs whose shape is known:

```go
type UserGetDetailIn struct {
	Username string `rfc:"USERNAME"`
}

type UserAddress struct {
	FullName string `rfc:"FULLNAME"`
}

type UserGetDetailOut struct {
	Address UserAddress              `rfc:"ADDRESS"`
	Return  []nwrfcparam.BAPIReturn  `rfc:"RETURN"`
}

var out UserGetDetailOut
raw, err := nwrfc.Call(ctx, conn, "BAPI_USER_GET_DETAIL",
	UserGetDetailIn{Username: username}, &out)
if err != nil {
	return err
}
if err := nwrfcparam.CheckRETURN(map[string]any(raw)); err != nil {
	return err
}
```

Use `nwrfc.CallMap` for exploratory calls, generic tooling, or BAPIs
whose structure is still being discovered:

```go
raw, err := nwrfc.CallMap(ctx, conn, "BAPI_USER_EXISTENCE_CHECK",
	map[string]any{"USERNAME": username})
```

Typed calls are the migration target for `nexus-spec`; dynamic calls are
the bridge while descriptors and generated packages are being completed.

## Error Handling

Transport, logon, ABAP, cancellation, marshaling, configuration, and SDK
availability failures are typed errors. Branch on sentinels for policy
and use `errors.As` when SAP detail is needed:

```go
if errors.Is(err, nwrfc.ErrLogon) {
	return fmt.Errorf("SAP logon failed: %w", err)
}

var abap *nwrfc.ABAPApplicationError
if errors.As(err, &abap) {
	slog.Warn("SAP ABAP application error",
		"msg_class", abap.AbapMsgClass,
		"msg_type", abap.AbapMsgType,
		"msg_number", abap.AbapMsgNumber)
}
```

BAPI business errors normally arrive in the conventional `RETURN`
parameter instead of as SDK errors. Use `nwrfcparam`:

```go
rows, err := nwrfcparam.ParseBAPIReturn(raw["RETURN"])
if err != nil {
	return err
}
if err := nwrfcparam.AsError(rows); err != nil {
	return err
}
```

`BAPIReturn.LogValue` redacts message variables because they can contain
business data.

## SDK-Free Tests

Install `nwrfcmock` in tests and compile with `-tags nwrfc_nosdk`:

```go
func TestUserGetDetail(t *testing.T) {
	mock := nwrfcmock.New()
	mock.HandleFunc("BAPI_USER_GET_DETAIL", func(ctx context.Context, in nwrfcmock.CallParams) (nwrfcmock.CallParams, error) {
		return nwrfcmock.CallParams{
			"ADDRESS": map[string]any{"FULLNAME": "Jane Doe"},
			"RETURN": []map[string]any{{"TYPE": "S", "ID": "01", "NUMBER": "000"}},
		}, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	conn, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "mock", SysNr: "00", Client: "100", User: "tester", Passwd: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var out UserGetDetailOut
	raw, err := nwrfc.Call(context.Background(), conn, "BAPI_USER_GET_DETAIL",
		UserGetDetailIn{Username: "JDOE"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if err := nwrfcparam.CheckRETURN(map[string]any(raw)); err != nil {
		t.Fatal(err)
	}
}
```

This pattern uses no SAP system, no SDK headers, and no committed
credentials.

## Code Generation

Use descriptors as the stable handoff between a SAP-connected developer
workstation and offline CI:

```bash
nwrfc-gen describe --fn BAPI_USER_GET_DETAIL --out descriptors/bapi_user_get_detail.json
nwrfc-gen generate --json descriptors/bapi_user_get_detail.json --pkg bapiuser --out internal/bapiuser/bapiuser.go
```

Generated packages can be kept current with `go generate`:

```go
//go:generate nwrfc-gen generate --json ../../descriptors/bapi_user_get_detail.json --pkg bapiuser --out bapiuser.go
```

The descriptors committed under `descriptors/` are reference artifacts for
the nexus-spec migration. When a customer SAP release differs, regenerate
them from that system and review the diff before updating generated code.

Current committed descriptor inventory:

- `descriptors/bapi_user_get_detail.json` for `BAPI_USER_GET_DETAIL`
- `descriptors/bapi_user_getlist.json` for `BAPI_USER_GETLIST`
- `descriptors/bapi_user_existence_check.json` for
  `BAPI_USER_EXISTENCE_CHECK`

`BAPI_PRGN_*` role/profile-management descriptors are not committed yet.
They must be captured from a SAP-connected workstation with the SAP NWRFC
SDK headers/libraries and `GORFC_TEST_*` connection variables configured,
for example:

```bash
nwrfc-gen describe --fn BAPI_PRGN_<FUNCTION> --out descriptors/bapi_prgn_<function>.json
```

This repository currently cannot claim the nexus-spec descriptor handoff is
complete for PRGN role/profile-management flows until those `bapi_prgn_*.json`
files are produced from a real test SAP system and reviewed. Do not create
synthetic PRGN descriptor JSON by hand; descriptor files must come from
`nwrfc-gen describe` or be explicitly marked as blocked.

## Large Tables

`nwrfc.Call` and `nwrfc.CallMap` materialize every returned table row
before returning. That is the simplest and safest path for small and
medium BAPI responses.

For large TABLES parameters, use `nwrfc.CallTableStream` so rows are read
lazily and the SDK function handle remains alive until `Close`:

```go
res, err := nwrfc.CallTableStream(ctx, conn, "BAPI_USER_GETLIST", "USERLIST", in)
if err != nil {
	return err
}
defer res.Close()

for {
	row, err := res.Next(ctx)
	if errors.Is(err, io.EOF) {
		break
	}
	if err != nil {
		return err
	}
	_ = row["USERNAME"]
}
```

While a stream is open, the `Conn` is pinned and must not be returned to a
pool. Close the stream on EOF, early break, and error paths.

## Observability

Core `nwrfc` has no OpenTelemetry dependency. For logging, use `slog` and
the redaction handler:

```go
base := slog.NewJSONHandler(os.Stderr, nil)
slog.SetDefault(slog.New(nwrfcotel.NewRedactHandler(base)))
```

Connection lifecycle events are opt-in listeners:

```go
nwrfc.AddListener(&nwrfcotel.ConnListener{Logger: slog.Default()})
```

Never log `nwrfc.Params`, SAP tickets, SNC material, x509 certificates,
or BAPI payloads with formatting that bypasses `slog.LogValuer`.

## SOAP to Native RFC

SOAP today:

```http
POST /sap/bc/soap/rfc
Content-Type: text/xml

<SOAP-ENV:Envelope>
  <SOAP-ENV:Body>
    <RFC:BAPI_USER_GET_DETAIL>
      <USERNAME>JDOE</USERNAME>
    </RFC:BAPI_USER_GET_DETAIL>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>
```

Native RFC target:

```go
type BAPIUserGetDetailIn struct {
	Username string `rfc:"USERNAME"`
}

type BAPIUserGetDetailOut struct {
	Address UserAddress             `rfc:"ADDRESS"`
	Return  []nwrfcparam.BAPIReturn `rfc:"RETURN"`
}

var out BAPIUserGetDetailOut
raw, err := nwrfc.Call(ctx, conn, "BAPI_USER_GET_DETAIL",
	BAPIUserGetDetailIn{Username: "JDOE"}, &out)
if err != nil {
	return err
}
if err := nwrfcparam.CheckRETURN(map[string]any(raw)); err != nil {
	return err
}
```

The caller keeps its existing SAP configuration ownership, but replaces
manual XML serialization and `RETURN` parsing with typed structs,
`context.Context`, and typed errors.
