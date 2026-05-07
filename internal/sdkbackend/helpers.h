/* SPDX-FileCopyrightText: 2026 gorfc community contributors
 * SPDX-License-Identifier: Apache-2.0
 *
 * Cross-file cgo helpers for the SAP NetWeaver RFC SDK binding.
 *
 * Each Go file in this package that does `import "C"` carries its
 * own preamble; cgo cannot see `static` helpers defined in the
 * preamble of a sibling file. The helpers therefore live in a
 * shared .c/.h pair (compiled by cgo as part of the package) and
 * every preamble that uses them does `#include "helpers.h"`.
 *
 * AGENTS.md §Engineering Rules: "Confirm memory allocation /
 * freeing for every C value crossing the CGO boundary." The
 * goMallocU / goFreeU pair is the only allocator we use for
 * SAP_UC buffers; the wrappers exist precisely so the pairing is
 * grep-able and reviewable in one place.
 */

#ifndef GORFC_SDKBACKEND_HELPERS_H
#define GORFC_SDKBACKEND_HELPERS_H

#include <sapnwrfc.h>
#include <stdlib.h>

/* goMallocU / goFreeU wrap the SDK's `mallocU` / `freeU` macros.
 * Pairing them explicitly (instead of using `C.free` on a
 * `mallocU` pointer) is portable across the SDK's per-OS macro
 * expansions. */
SAP_UC* goMallocU(unsigned size);
void    goFreeU(SAP_UC* p);

/* goStrlenU returns the SAP_UC string length (in code units)
 * excluding the trailing null. Wraps `strlenU` from <sapuc.h>. */
unsigned goStrlenU(SAP_UC* s);

/* goSetParamActive forwards to RfcSetParameterActive, normalizing
 * the active flag to 0/1. Used for the `notRequested` filter. */
RFC_RC goSetParamActive(RFC_FUNCTION_HANDLE h,
                        SAP_UC* name,
                        int active,
                        RFC_ERROR_INFO* info);

#endif /* GORFC_SDKBACKEND_HELPERS_H */
