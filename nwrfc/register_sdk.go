// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package nwrfc

// Importing the cgo SDK backend for the side effect of its
// init() registration. Build tags ensure exactly one backend
// is linked per build.
import _ "github.com/cjordaoc/gorfc/internal/sdkbackend"
