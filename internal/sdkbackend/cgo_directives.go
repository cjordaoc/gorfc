// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// cgo build directives for the SAP NetWeaver RFC SDK.
//
// Users set CGO_CFLAGS / CGO_LDFLAGS at the build site to point
// at their installed SDK; this file declares the per-OS flags
// the SDK requires (defines, alignment, threading) but does NOT
// hardcode any path. Examples:
//
//	# Linux
//	export CGO_CFLAGS="-I$SAPNWRFC_HOME/include"
//	export CGO_LDFLAGS="-L$SAPNWRFC_HOME/lib -Wl,-rpath,$SAPNWRFC_HOME/lib"
//
//	# Windows (MinGW-w64)
//	set CGO_CFLAGS=-IC:\nwrfcsdk\include
//	set CGO_LDFLAGS=-LC:\nwrfcsdk\lib
//
//	# macOS
//	export CGO_CFLAGS="-I/opt/sap/nwrfcsdk/include"
//	export CGO_LDFLAGS="-L/opt/sap/nwrfcsdk/lib -Wl,-rpath,/opt/sap/nwrfcsdk/lib"
//
// Builds without these env vars set, on platforms where the SDK
// is not present, fail explicitly at the cgo header step
// (`fatal error: sapnwrfc.h: No such file or directory`) — this
// is the AGENTS.md "no silent fallback" contract: operators
// see the missing-SDK condition immediately.

package sdkbackend

/*
#cgo linux CFLAGS: -DNDEBUG -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64
#cgo linux CFLAGS: -DSAPwithUNICODE -D__NO_MATH_INLINES -DSAPwithTHREADS
#cgo linux CFLAGS: -DSAPonUNIX -DSAPonLIN
#cgo linux CFLAGS: -fno-strict-aliasing -fno-omit-frame-pointer -fexceptions -funsigned-char
#cgo linux CFLAGS: -Wall -Wno-uninitialized -Wno-long-long -Wcast-align -Wno-unused-variable
#cgo linux LDFLAGS: -lsapnwrfc -lsapucum -pthread

#cgo windows CFLAGS: -DSAPonNT -DSAPwithUNICODE -DUNICODE -D_UNICODE
#cgo windows CFLAGS: -DSAPwithTHREADS -DNDEBUG -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64
#cgo windows CFLAGS: -fno-strict-aliasing -fexceptions -funsigned-char
#cgo windows LDFLAGS: -lsapnwrfc -llibsapucum

#cgo darwin CFLAGS: -DSAP_UC_is_wchar -DSAPwithUNICODE -D__NO_MATH_INLINES
#cgo darwin CFLAGS: -DSAPwithTHREADS -DSAPonDARW
#cgo darwin CFLAGS: -fexceptions -funsigned-char -fno-strict-aliasing -fPIC -pthread
#cgo darwin CFLAGS: -mmacosx-version-min=10.15
#cgo darwin LDFLAGS: -lsapnwrfc -lsapucum -mmacosx-version-min=10.15

// goMallocU / goFreeU / goStrlenU / goSetParamActive live in
// helpers.c+helpers.h so they have package-wide linkage. Defining
// them as `static` in this preamble would hide them from the
// other .go files in the package (each cgo Go file carries its
// own preamble). Each cgo file in this package now does
// `#include "helpers.h"` instead of including <sapnwrfc.h>
// directly.
#include "helpers.h"
*/
import "C"
