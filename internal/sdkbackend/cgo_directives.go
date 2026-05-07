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

#include <sapnwrfc.h>
#include <stdlib.h>

// goMallocU / goFreeU are paired allocators that wrap the SDK's
// `mallocU` / `freeU` macros, used for SAP_UC buffers. Pairing
// these explicitly (instead of using `C.free` on a `mallocU`
// pointer) is portable; on Linux/Windows the SDK's macros
// expand to system malloc/free anyway.
static SAP_UC* goMallocU(unsigned size) { return mallocU(size); }
static void goFreeU(SAP_UC* p) { if (p) freeU(p); }

// goStrlenU returns the SAP_UC string length (in code units)
// without the trailing null. Wraps `strlenU` from <sapuc.h>.
static unsigned goStrlenU(SAP_UC* s) { return strlenU(s); }

// goCallParamActive sets a parameter to inactive
// (notRequested) before RfcInvoke. Wraps the SDK's
// `RfcSetParameterActive` to keep the cgo files free of the
// active-flag transcoding boilerplate.
static RFC_RC goSetParamActive(RFC_FUNCTION_HANDLE h,
                                SAP_UC* name,
                                int active,
                                RFC_ERROR_INFO* info) {
    return RfcSetParameterActive(h, name, active ? 1 : 0, info);
}
*/
import "C"
