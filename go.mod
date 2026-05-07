// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0
//
// Module path migrated from `github.com/sap/gorfc` (archived) to a
// community-owned namespace. The legacy package import shim
// (`compat/gorfc`) lands in T1.14 to keep upstream callers building
// for one minor release; see docs/PORTING_STRATEGY.md.

module github.com/cjordaoc/gorfc

go 1.23

toolchain go1.25.0

require github.com/stretchr/testify v1.10.0

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
