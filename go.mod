// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0
//
// Module path migrated from `github.com/sap/gorfc` (archived) to a
// community-owned namespace. The legacy package import shim
// (`compat/gorfc`) lands in T1.14 to keep upstream callers building
// for one minor release; see docs/PORTING_STRATEGY.md.

module github.com/cjordaoc/gorfc

// Go 1.25 is required for the testing/synctest API used by the
// cancel/timeout tests under nwrfc/cancel_synctest_test.go. The
// toolchain directive auto-fetches 1.25.0 if the developer's
// machine has an older go.
go 1.25

toolchain go1.25.0

require github.com/stretchr/testify v1.10.0

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
