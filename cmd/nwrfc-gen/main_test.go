// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
)

func TestGenerateSeparatesChangingParams(t *testing.T) {
	files, err := generate("stfc", backend.FunctionDescriptor{
		Name: "Z_CHANGING",
		Parameters: []backend.ParameterDescriptor{
			{Name: "REQUTEXT", Type: backend.TypeChar, Direction: backend.DirImport},
			{Name: "STATE", Type: backend.TypeChar, Direction: backend.DirChanging},
			{Name: "RESPTEXT", Type: backend.TypeChar, Direction: backend.DirExport},
		},
	}, false)
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
	}, false)
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
	}, false)
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

	// Resolve the generated module's dependency graph: the bare
	// go.mod above only names github.com/cjordaoc/gorfc, but the
	// generated test transitively needs that module's own
	// requires recorded too.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy in generated module failed: %v\n%s", err, string(out))
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

// scalarBenchDescriptor is a BAPI-shaped descriptor with five
// scalar parameters in each direction. It backs both the --fast
// codegen tests and the reflection-vs-fast benchmarks below.
func scalarBenchDescriptor() backend.FunctionDescriptor {
	return backend.FunctionDescriptor{
		Name: "Z_BENCH_SCALARS",
		Parameters: []backend.ParameterDescriptor{
			{Name: "REQ_TEXT", Type: backend.TypeChar, Direction: backend.DirImport, Length: 32},
			{Name: "REQ_COUNT", Type: backend.TypeInt, Direction: backend.DirImport},
			{Name: "REQ_FLAG", Type: backend.TypeChar, Direction: backend.DirImport, Length: 1},
			{Name: "REQ_ID", Type: backend.TypeInt8, Direction: backend.DirImport},
			{Name: "REQ_SMALL", Type: backend.TypeInt2, Direction: backend.DirImport},
			{Name: "RESP_TEXT", Type: backend.TypeChar, Direction: backend.DirExport, Length: 32},
			{Name: "RESP_COUNT", Type: backend.TypeInt, Direction: backend.DirExport},
			{Name: "RESP_FLAG", Type: backend.TypeChar, Direction: backend.DirExport, Length: 1},
			{Name: "RESP_ID", Type: backend.TypeInt8, Direction: backend.DirExport},
			{Name: "RESP_SMALL", Type: backend.TypeInt2, Direction: backend.DirExport},
		},
	}
}

// TestGenerateFast_EmitsNoReflect verifies that --fast output
// carries no "reflect" import: the whole point of the fast path
// is that marshal/unmarshal are reflection-free.
func TestGenerateFast_EmitsNoReflect(t *testing.T) {
	files, err := generate("benchfast", scalarBenchDescriptor(), true)
	if err != nil {
		t.Fatalf("generate --fast: %v", err)
	}
	src := string(files.Source)
	if strings.Contains(src, `"reflect"`) {
		t.Fatalf("--fast source imports reflect:\n%s", src)
	}
	for _, want := range []string{
		"func marshalIn(in In) backend.CallParams",
		"func unmarshalOut(raw backend.CallParams, out *Out)",
		"func CallFast(ctx context.Context, conn *nwrfc.Conn, in In) (Out, error)",
		"nwrfc.CallMap(ctx, conn,",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("--fast source missing %q\n%s", want, src)
		}
	}
}

// TestGenerateFast_Compiles generates --fast code, writes it into
// a temp package inside the module (so the generated internal
// import resolves), and verifies it compiles with `go build`.
func TestGenerateFast_Compiles(t *testing.T) {
	files, err := generate("benchfast", scalarBenchDescriptor(), true)
	if err != nil {
		t.Fatalf("generate --fast: %v", err)
	}
	// The temp dir must live inside the module tree: the
	// generated code imports github.com/cjordaoc/gorfc/internal/
	// backend, which Go only lets packages under the same module
	// root import.
	tmpDir, err := os.MkdirTemp(".", "fastgentest")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	if err := os.WriteFile(filepath.Join(tmpDir, "generated.go"), files.Source, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "generated_test.go"), files.Test, 0o644); err != nil {
		t.Fatalf("write test: %v", err)
	}

	cmd := exec.Command("go", "build", "-tags", "nwrfc_nosdk", "./"+filepath.Base(tmpDir))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build of --fast output failed: %v\n%s", err, string(out))
	}
}

// =====================================================================
// SDK-free benchmarks: reflection-based nwrfc.Call vs. the
// reflection-free path emitted by nwrfc-gen --fast.
// =====================================================================

const benchFn = "Z_BENCH_SCALARS"

type benchIn struct {
	ReqText  string `rfc:"REQ_TEXT"`
	ReqCount int32  `rfc:"REQ_COUNT"`
	ReqFlag  string `rfc:"REQ_FLAG"`
	ReqID    int64  `rfc:"REQ_ID"`
	ReqSmall int16  `rfc:"REQ_SMALL"`
}

type benchOut struct {
	RespText  string `rfc:"RESP_TEXT"`
	RespCount int32  `rfc:"RESP_COUNT"`
	RespFlag  string `rfc:"RESP_FLAG"`
	RespID    int64  `rfc:"RESP_ID"`
	RespSmall int16  `rfc:"RESP_SMALL"`
}

// marshalBenchIn / unmarshalBenchOut mirror, by hand, exactly the
// reflection-free code that `nwrfc-gen --fast` emits for benchIn /
// benchOut. Keeping the mirror here lets the benchmark compare
// the two code paths without a codegen+compile step on every run.
func marshalBenchIn(in benchIn) backend.CallParams {
	raw := make(backend.CallParams, 5)
	raw["REQ_TEXT"] = in.ReqText
	raw["REQ_COUNT"] = in.ReqCount
	raw["REQ_FLAG"] = in.ReqFlag
	raw["REQ_ID"] = in.ReqID
	raw["REQ_SMALL"] = in.ReqSmall
	return raw
}

func unmarshalBenchOut(raw backend.CallParams, out *benchOut) {
	if v, ok := raw["RESP_TEXT"]; ok {
		if tv, ok := v.(string); ok {
			out.RespText = tv
		}
	}
	if v, ok := raw["RESP_COUNT"]; ok {
		if tv, ok := v.(int32); ok {
			out.RespCount = tv
		}
	}
	if v, ok := raw["RESP_FLAG"]; ok {
		if tv, ok := v.(string); ok {
			out.RespFlag = tv
		}
	}
	if v, ok := raw["RESP_ID"]; ok {
		if tv, ok := v.(int64); ok {
			out.RespID = tv
		}
	}
	if v, ok := raw["RESP_SMALL"]; ok {
		if tv, ok := v.(int16); ok {
			out.RespSmall = tv
		}
	}
}

// newBenchConn installs a mock backend with an echo handler for
// benchFn and returns an open Conn. SDK-free; works under
// -tags nwrfc_nosdk.
func newBenchConn(b *testing.B) *nwrfc.Conn {
	b.Helper()
	mock := nwrfcmock.New()
	mock.HandleFunc(benchFn, func(_ context.Context, in backend.CallParams) (backend.CallParams, error) {
		return backend.CallParams{
			"RESP_TEXT":  in["REQ_TEXT"],
			"RESP_COUNT": in["REQ_COUNT"],
			"RESP_FLAG":  in["REQ_FLAG"],
			"RESP_ID":    in["REQ_ID"],
			"RESP_SMALL": in["REQ_SMALL"],
		}, nil
	})
	restore := nwrfcmock.Install(mock)
	b.Cleanup(restore)

	conn, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = conn.Close() })
	return conn
}

// BenchmarkCallReflection exercises the current path: nwrfc.Call
// with reflection-based marshalInput / unmarshalOutput.
func BenchmarkCallReflection(b *testing.B) {
	conn := newBenchConn(b)
	ctx := context.Background()
	in := benchIn{ReqText: "ping", ReqCount: 7, ReqFlag: "X", ReqID: 42, ReqSmall: 3}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out benchOut
		if _, err := nwrfc.Call(ctx, conn, benchFn, in, &out); err != nil {
			b.Fatalf("Call: %v", err)
		}
	}
}

// BenchmarkCallFast exercises the generated --fast path: typed,
// reflection-free marshalBenchIn / unmarshalBenchOut around
// nwrfc.CallMap.
func BenchmarkCallFast(b *testing.B) {
	conn := newBenchConn(b)
	ctx := context.Background()
	in := benchIn{ReqText: "ping", ReqCount: 7, ReqFlag: "X", ReqID: 42, ReqSmall: 3}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out benchOut
		raw, err := nwrfc.CallMap(ctx, conn, benchFn, marshalBenchIn(in))
		if err != nil {
			b.Fatalf("CallMap: %v", err)
		}
		unmarshalBenchOut(raw, &out)
	}
}
