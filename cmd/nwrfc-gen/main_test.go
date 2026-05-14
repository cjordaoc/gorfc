// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
)

func TestGenerateSeparatesChangingParams(t *testing.T) {
	files, err := generate("stfc", backend.FunctionDescriptor{
		Name: "Z_CHANGING",
		Parameters: []backend.ParameterDescriptor{
			{Name: "REQUTEXT", Type: backend.TypeChar, Direction: backend.DirImport},
			{Name: "STATE", Type: backend.TypeChar, Direction: backend.DirChanging},
			{Name: "RESPTEXT", Type: backend.TypeChar, Direction: backend.DirExport},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src := string(files.Source)
	if got := strings.Count(src, "`rfc:\"STATE\"`"); got != 1 {
		t.Fatalf("CHANGING field occurrences=%d want 1\n%s", got, src)
	}
	for _, want := range []string{
		"type InOut struct",
		"type In struct",
		"type Out struct",
		"func Call(ctx context.Context, conn *nwrfc.Conn, in In) (Out, error)",
		"func CallWithPool(ctx context.Context, pool *nwrfc.Pool, in In) (Out, error)",
		"pool.Do(ctx, func(conn *nwrfc.Conn) error",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("generated source missing %q\n%s", want, src)
		}
	}
}

func TestGenerateRecursiveNestedDescriptorDoesNotPanic(t *testing.T) {
	root := &backend.TypeDescriptor{Name: "ROOT"}
	child := &backend.TypeDescriptor{Name: "CHILD"}
	root.Fields = []backend.FieldDescriptor{
		{Name: "CHILD", Type: backend.TypeStructure, TypeDesc: child},
	}
	child.Fields = []backend.FieldDescriptor{
		{Name: "PARENT", Type: backend.TypeStructure, TypeDesc: root},
	}

	files, err := generate("nested", backend.FunctionDescriptor{
		Name: "Z_NESTED",
		Parameters: []backend.ParameterDescriptor{
			{Name: "ROOT", Type: backend.TypeStructure, Direction: backend.DirImport, TypeDesc: root},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(string(files.Source), "Parent map[string]any") {
		t.Fatalf("cycle was not broken with map[string]any\n%s", string(files.Source))
	}
}

func TestGeneratedPackageCompilesAndRunsSDKFreeTest(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	files, err := generate("stfc", backend.FunctionDescriptor{
		Name: "STFC_CONNECTION",
		Parameters: []backend.ParameterDescriptor{
			{Name: "REQUTEXT", Type: backend.TypeChar, Direction: backend.DirImport},
			{Name: "ECHOTEXT", Type: backend.TypeChar, Direction: backend.DirExport},
			{Name: "RESPTEXT", Type: backend.TypeChar, Direction: backend.DirExport},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated.test\n\ngo 1.23\n\nrequire github.com/cjordaoc/gorfc v0.0.0\n\nreplace github.com/cjordaoc/gorfc => "+repoRoot+"\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stfc_connection.go"), files.Source, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stfc_connection_test.go"), files.Test, 0o644); err != nil {
		t.Fatalf("write test: %v", err)
	}

	cmd := exec.Command("go", "test", "-tags", "nwrfc_nosdk", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated go test failed: %v\n%s", err, string(out))
	}

	cmd = exec.Command("go", "vet", "-tags", "nwrfc_nosdk", "./...")
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated go vet failed: %v\n%s", err, string(out))
	}
}

func TestGoIdentifierDoesNotUseStringsTitleBehavior(t *testing.T) {
	if got := goIdentifier("/bapi-user_2"); got != "BapiUser2" {
		t.Fatalf("goIdentifier=%q", got)
	}
}
