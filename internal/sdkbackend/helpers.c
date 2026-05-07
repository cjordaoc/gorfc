/* SPDX-FileCopyrightText: 2026 gorfc community contributors
 * SPDX-License-Identifier: Apache-2.0
 *
 * See helpers.h for the rationale.
 */

#include "helpers.h"

SAP_UC* goMallocU(unsigned size) {
    return mallocU(size);
}

/* The SAP SDK exposes `mallocU` as a macro that expands to
 * `mallocU16(len)` → `(SAP_UTF16*) malloc(len * sizeof(SAP_UTF16))`
 * (see sapucrfc.h §1869-1870). There is no matching `freeU` macro
 * in the public header set. Pairing `mallocU` with the standard
 * `free()` is therefore correct on every platform the SDK
 * supports (Linux, Windows, macOS), which is also what the legacy
 * `gorfc/gorfc.go` does (see its 224-227 comment block). The
 * wrapper exists to keep the malloc/free pairing grep-able and
 * AGENTS.md-compliant. */
void goFreeU(SAP_UC* p) {
    if (p) free(p);
}

unsigned goStrlenU(SAP_UC* s) {
    return strlenU(s);
}

RFC_RC goSetParamActive(RFC_FUNCTION_HANDLE h,
                        SAP_UC* name,
                        int active,
                        RFC_ERROR_INFO* info) {
    return RfcSetParameterActive(h, name, active ? 1 : 0, info);
}
