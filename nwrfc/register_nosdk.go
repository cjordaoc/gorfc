// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !cgo || nwrfc_nosdk

package nwrfc

// Importing the no-SDK backend purely for the side effect of
// its init() function (which registers itself with the
// backend.Default registry). Build tags ensure exactly one
// backend registers per build.
import _ "github.com/cjordaoc/gorfc/internal/nosdkbackend"
