// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// cgo build directives for the SAP NetWeaver RFC SDK.
//
// The defaults here read SAPNWRFC_HOME from the environment at
// build time (via -ldflags or -gcflags injection at the user
// site, or by exporting CGO_CFLAGS / CGO_LDFLAGS before
// `go build`). Hardcoded paths from upstream `gorfc/gorfc.go`
// are removed; users now configure their toolchain explicitly,
// per AGENTS.md "build configuration must be documented and
// portable".
//
// Operator guide for setting CGO_CFLAGS / CGO_LDFLAGS lives in
// docs/INSTALL.md (T1.15).

package sdkbackend

// Linux:
//
// #cgo linux CFLAGS: -DNDEBUG -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64
// #cgo linux CFLAGS: -DSAPwithUNICODE -D__NO_MATH_INLINES -DSAPwithTHREADS
// #cgo linux CFLAGS: -DSAPonUNIX -DSAPonLIN
// #cgo linux CFLAGS: -O2 -g -pthread -pipe -m64
// #cgo linux CFLAGS: -fno-strict-aliasing -fno-omit-frame-pointer -fexceptions -funsigned-char
// #cgo linux CFLAGS: -Wall -Wno-uninitialized -Wno-long-long
// #cgo linux CFLAGS: -Wcast-align -Wno-unused-variable
// #cgo linux LDFLAGS: -lsapnwrfc -lsapucum -O2 -g -pthread
//
// Linux include and library search paths come from CGO_CFLAGS
// and CGO_LDFLAGS that the user sets from $SAPNWRFC_HOME.
// Example:
//
//   export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
//   export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,$SAPNWRFC_HOME/lib"
//   go build -tags cgo ./...
//
// Windows (x86_64, MinGW-w64):
//
// #cgo windows CFLAGS: -DSAPonNT -DSAPwithUNICODE -DUNICODE -D_UNICODE
// #cgo windows CFLAGS: -DSAPwithTHREADS -DNDEBUG -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64
// #cgo windows CFLAGS: -O2 -g -m64 -fno-strict-aliasing -fexceptions -funsigned-char
// #cgo windows LDFLAGS: -lsapnwrfc -llibsapucum
//
// Windows users set CGO_CFLAGS/CGO_LDFLAGS to point at the
// extracted SDK directory (e.g. C:\Tools\nwrfcsdk).
//
// Darwin (best-effort, tier-2):
//
// #cgo darwin CFLAGS: -DSAP_UC_is_wchar -DSAPwithUNICODE -D__NO_MATH_INLINES
// #cgo darwin CFLAGS: -DSAPwithTHREADS -DSAPonDARW
// #cgo darwin CFLAGS: -O2 -g -fexceptions -funsigned-char -fno-strict-aliasing -fPIC -pthread
// #cgo darwin CFLAGS: -mmacosx-version-min=10.15
// #cgo darwin LDFLAGS: -lsapnwrfc -lsapucum -mmacosx-version-min=10.15
//
// The cgo directives above are commented out so the skeleton
// build does not require SAP headers in CI. The real directives
// are uncommented in T1.5 once the bindings need to compile
// against the SDK headers; until then, the skeleton compiles
// against whatever CGO_CFLAGS/CGO_LDFLAGS the user has set, and
// `cgo` is happy because `backend_sdk.go` includes the headers
// only when those env vars resolve them.
