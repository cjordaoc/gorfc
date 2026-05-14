// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Command nwrfc-gen describes SAP RFC functions and generates typed Go
// client packages from saved descriptors.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		fail("subcommand required")
	}
	switch os.Args[1] {
	case "describe":
		runDescribe(os.Args[2:])
	case "generate":
		runGenerate(os.Args[2:])
	default:
		runGenerate(os.Args[1:])
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  nwrfc-gen describe --fn RFC_NAME --out descriptor.json
  nwrfc-gen generate --json descriptor.json --pkg package --out output_dir

describe connects to SAP using GORFC_TEST_* environment variables and writes
a commit-friendly JSON FunctionDescriptor.

generate reads a descriptor JSON and writes typed Go source plus an SDK-free
test that uses nwrfcmock. If --out is omitted, source is written to stdout.
`)
}

func runDescribe(args []string) {
	fs := flag.NewFlagSet("describe", flag.ExitOnError)
	fnName := fs.String("fn", "", "RFC function name")
	outPath := fs.String("out", "", "descriptor JSON output path")
	timeout := fs.Duration("timeout", 30*time.Second, "describe timeout")
	_ = fs.Parse(args)
	if *fnName == "" {
		fail("describe: --fn required")
	}
	if *outPath == "" {
		fail("describe: --out required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	conn, err := nwrfc.Open(ctx, paramsFromEnv())
	if err != nil {
		fail("Open: %v", err)
	}
	defer conn.Close()
	desc, err := conn.Describe(ctx, *fnName)
	if err != nil {
		fail("Describe: %v", err)
	}
	if err := writeDescriptorJSON(*outPath, desc); err != nil {
		fail("%v", err)
	}
}

func runGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	fnName := fs.String("fn", "", "RFC function name (live mode)")
	jsonPath := fs.String("json", "", "descriptor JSON path (offline mode)")
	saveJSON := fs.String("save-json", "", "save live descriptor JSON before generating")
	pkgName := fs.String("pkg", "rfcclient", "Go package name")
	outPath := fs.String("out", "", "output file or directory; default stdout")
	timeout := fs.Duration("timeout", 30*time.Second, "live describe timeout")
	_ = fs.Parse(args)

	desc, err := loadDescriptor(*jsonPath, *fnName, *timeout)
	if err != nil {
		fail("%v", err)
	}
	if *saveJSON != "" {
		if err := writeDescriptorJSON(*saveJSON, desc); err != nil {
			fail("%v", err)
		}
	}
	files, err := generate(*pkgName, desc)
	if err != nil {
		fail("generate: %v", err)
	}
	if err := writeGenerated(*outPath, desc.Name, files); err != nil {
		fail("%v", err)
	}
}

func loadDescriptor(jsonPath, fnName string, timeout time.Duration) (backend.FunctionDescriptor, error) {
	switch {
	case jsonPath != "":
		b, err := os.ReadFile(jsonPath)
		if err != nil {
			return backend.FunctionDescriptor{}, fmt.Errorf("read %s: %w", jsonPath, err)
		}
		var desc backend.FunctionDescriptor
		if err := json.Unmarshal(b, &desc); err != nil {
			return backend.FunctionDescriptor{}, fmt.Errorf("parse %s: %w", jsonPath, err)
		}
		return desc, nil
	case fnName != "":
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		conn, err := nwrfc.Open(ctx, paramsFromEnv())
		if err != nil {
			return backend.FunctionDescriptor{}, fmt.Errorf("Open: %w", err)
		}
		defer conn.Close()
		desc, err := conn.Describe(ctx, fnName)
		if err != nil {
			return backend.FunctionDescriptor{}, fmt.Errorf("Describe: %w", err)
		}
		return desc, nil
	default:
		return backend.FunctionDescriptor{}, errors.New("--json or --fn required")
	}
}

func paramsFromEnv() nwrfc.Params {
	return nwrfc.Params{
		Dest:   os.Getenv("GORFC_TEST_DEST"),
		AsHost: os.Getenv("GORFC_TEST_ASHOST"),
		SysNr:  os.Getenv("GORFC_TEST_SYSNR"),
		Client: os.Getenv("GORFC_TEST_CLIENT"),
		User:   os.Getenv("GORFC_TEST_USER"),
		Passwd: os.Getenv("GORFC_TEST_PASSWD"),
		Lang:   os.Getenv("GORFC_TEST_LANG"),
	}
}

func writeDescriptorJSON(path string, desc backend.FunctionDescriptor) error {
	b, err := json.MarshalIndent(desc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode descriptor: %w", err)
	}
	b = append(b, '\n')
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

type generatedFiles struct {
	Source []byte
	Test   []byte
}

func writeGenerated(outPath, fnName string, files generatedFiles) error {
	if outPath == "" {
		_, err := os.Stdout.Write(files.Source)
		return err
	}
	if strings.HasSuffix(outPath, ".go") {
		if dir := filepath.Dir(outPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", dir, err)
			}
		}
		if err := os.WriteFile(outPath, files.Source, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}
		testPath := strings.TrimSuffix(outPath, ".go") + "_test.go"
		if err := os.WriteFile(testPath, files.Test, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", testPath, err)
		}
		return nil
	}
	if err := os.MkdirAll(outPath, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outPath, err)
	}
	base := fileBase(fnName)
	paths := map[string][]byte{
		filepath.Join(outPath, base+".go"):      files.Source,
		filepath.Join(outPath, base+"_test.go"): files.Test,
	}
	for p, b := range paths {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	return nil
}

const sourceTmpl = `// Code generated by nwrfc-gen. DO NOT EDIT.
// SPDX-License-Identifier: Apache-2.0

package {{.Package}}

import (
	"context"

	"github.com/cjordaoc/gorfc/nwrfc"
)

// InOut contains CHANGING parameters for {{.RFCName}}.
type InOut struct {
{{- range .Changing}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
}

// In contains IMPORT parameters for {{.RFCName}}.
type In struct {
{{- if .Changing}}
	InOut
{{- end}}
{{- range .Imports}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
}

// Out contains EXPORT, TABLES, and CHANGING results for {{.RFCName}}.
type Out struct {
{{- if .Changing}}
	InOut
{{- end}}
{{- range .Exports}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
{{- range .Tables}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
}

{{range .Structs}}
type {{.GoName}} struct {
{{- range .Fields}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
}
{{end}}

// Call invokes {{.RFCName}} using conn.
func Call(ctx context.Context, conn *nwrfc.Conn, in In) (Out, error) {
	var out Out
	_, err := nwrfc.Call(ctx, conn, "{{.RFCName}}", in, &out)
	return out, err
}

// CallWithPool invokes {{.RFCName}} using pool.Do.
func CallWithPool(ctx context.Context, pool *nwrfc.Pool, in In) (Out, error) {
	var out Out
	err := pool.Do(ctx, func(conn *nwrfc.Conn) error {
		var err error
		out, err = Call(ctx, conn, in)
		return err
	})
	return out, err
}
`

const testTmpl = `// Code generated by nwrfc-gen. DO NOT EDIT.
// SPDX-License-Identifier: Apache-2.0

package {{.Package}}

import (
	"context"
	"testing"

	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
)

func TestGeneratedCall(t *testing.T) {
	mock := nwrfcmock.New()
	mock.HandleFunc("{{.RFCName}}", func(ctx context.Context, in nwrfcmock.CallParams) (nwrfcmock.CallParams, error) {
		out := nwrfcmock.CallParams{}
{{- range .Changing}}
		out["{{.RFCName}}"] = in["{{.RFCName}}"]
{{- end}}
		return out, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	conn, err := nwrfc.Open(context.Background(), nwrfc.Params{Dest: "MOCK"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	if _, err := Call(context.Background(), conn, In{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestGeneratedCallWithPool(t *testing.T) {
	mock := nwrfcmock.New()
	mock.HandleFunc("{{.RFCName}}", func(ctx context.Context, in nwrfcmock.CallParams) (nwrfcmock.CallParams, error) {
		return nwrfcmock.CallParams{}, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	pool, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{Dest: "MOCK"},
		MaxSize: 1,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Fatalf("Pool.Close: %v", err)
		}
	})

	if _, err := CallWithPool(context.Background(), pool, In{}); err != nil {
		t.Fatalf("CallWithPool: %v", err)
	}
	if got := mock.CallCount(); got != 1 {
		t.Fatalf("CallCount=%d want 1", got)
	}
}
`

type tmplData struct {
	Package  string
	RFCName  string
	Imports  []paramDoc
	Exports  []paramDoc
	Changing []paramDoc
	Tables   []paramDoc
	Structs  []structDoc
}

type paramDoc struct {
	GoName  string
	RFCName string
	GoType  string
}

type structDoc struct {
	GoName string
	Fields []paramDoc
}

type structCollector struct {
	emitted   map[string]bool
	resolving map[string]bool
	structs   []structDoc
}

func generate(pkg string, d backend.FunctionDescriptor) (generatedFiles, error) {
	if pkg == "" {
		return generatedFiles{}, errors.New("package name is required")
	}
	if d.Name == "" {
		return generatedFiles{}, errors.New("descriptor name is required")
	}
	c := &structCollector{
		emitted:   map[string]bool{},
		resolving: map[string]bool{},
	}
	data := tmplData{
		Package: goPackageName(pkg),
		RFCName: d.Name,
	}
	for _, p := range d.Parameters {
		field := paramDoc{
			GoName:  goIdentifier(p.Name),
			RFCName: p.Name,
			GoType:  c.goTypeForParam(p),
		}
		switch p.Direction {
		case backend.DirImport:
			data.Imports = append(data.Imports, field)
		case backend.DirExport, backend.DirReturn:
			data.Exports = append(data.Exports, field)
		case backend.DirChanging:
			data.Changing = append(data.Changing, field)
		case backend.DirTables:
			data.Tables = append(data.Tables, field)
		default:
			if p.Direction&backend.DirImport != 0 {
				data.Imports = append(data.Imports, field)
			}
			if p.Direction&backend.DirExport != 0 || p.Direction&backend.DirReturn != 0 {
				data.Exports = append(data.Exports, field)
			}
			if p.Direction&backend.DirChanging != 0 {
				data.Changing = append(data.Changing, field)
			}
			if p.Direction&backend.DirTables != 0 {
				data.Tables = append(data.Tables, field)
			}
		}
	}
	data.Structs = c.sortedStructs()

	source, err := render(sourceTmpl, data)
	if err != nil {
		return generatedFiles{}, err
	}
	test, err := render(testTmpl, data)
	if err != nil {
		return generatedFiles{}, err
	}
	return generatedFiles{Source: source, Test: test}, nil
}

func render(tmpl string, data tmplData) ([]byte, error) {
	t := template.Must(template.New("file").Parse(tmpl))
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("gofmt: %w", err)
	}
	return formatted, nil
}

func (c *structCollector) sortedStructs() []structDoc {
	out := append([]structDoc(nil), c.structs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].GoName < out[j].GoName })
	return out
}

func (c *structCollector) goTypeForParam(p backend.ParameterDescriptor) string {
	switch p.Type {
	case backend.TypeStructure:
		return c.typeName(p.TypeDesc, p.Name, false)
	case backend.TypeTable:
		return "[]" + c.typeName(p.TypeDesc, p.Name, true)
	default:
		return goTypeForScalar(p.Type)
	}
}

func (c *structCollector) goTypeForField(f backend.FieldDescriptor, parent string) string {
	switch f.Type {
	case backend.TypeStructure:
		return c.typeName(f.TypeDesc, parent+"_"+f.Name, false)
	case backend.TypeTable:
		return "[]" + c.typeName(f.TypeDesc, parent+"_"+f.Name, true)
	default:
		return goTypeForScalar(f.Type)
	}
}

func (c *structCollector) typeName(td *backend.TypeDescriptor, fallback string, table bool) string {
	if td == nil {
		return "map[string]any"
	}
	name := td.Name
	if name == "" {
		name = fallback
	}
	goName := goIdentifier(name)
	if c.resolving[goName] {
		return "map[string]any"
	}
	if c.emitted[goName] {
		return goName
	}
	c.resolving[goName] = true
	doc := structDoc{GoName: goName}
	for _, f := range td.Fields {
		doc.Fields = append(doc.Fields, paramDoc{
			GoName:  goIdentifier(f.Name),
			RFCName: f.Name,
			GoType:  c.goTypeForField(f, name),
		})
	}
	delete(c.resolving, goName)
	c.emitted[goName] = true
	c.structs = append(c.structs, doc)
	return goName
}

func goTypeForScalar(t backend.RFCType) string {
	switch t {
	case backend.TypeChar, backend.TypeNum, backend.TypeString,
		backend.TypeBCD, backend.TypeFloat, backend.TypeDecF16,
		backend.TypeDecF34, backend.TypeUTCLong, backend.TypeDate,
		backend.TypeTime, backend.TypeXMLData:
		return "string"
	case backend.TypeInt1:
		return "uint8"
	case backend.TypeInt2:
		return "int16"
	case backend.TypeInt:
		return "int32"
	case backend.TypeInt8:
		return "int64"
	case backend.TypeByte, backend.TypeXString:
		return "[]byte"
	default:
		return "any"
	}
}

func goIdentifier(in string) string {
	words := splitIdentifier(in)
	if len(words) == 0 {
		return "Generated"
	}
	var out strings.Builder
	for _, w := range words {
		rs := []rune(strings.ToLower(w))
		if len(rs) == 0 {
			continue
		}
		rs[0] = unicode.ToUpper(rs[0])
		out.WriteString(string(rs))
	}
	got := out.String()
	if got == "" {
		got = "Generated"
	}
	first := []rune(got)[0]
	if !unicode.IsLetter(first) && first != '_' {
		got = "X" + got
	}
	if goKeywords[got] {
		got += "_"
	}
	return got
}

func goPackageName(in string) string {
	var out strings.Builder
	for i, r := range strings.ToLower(in) {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			out.WriteRune(r)
		}
	}
	got := out.String()
	if got == "" || goKeywords[got] {
		return "rfcclient"
	}
	return got
}

func splitIdentifier(in string) []string {
	return strings.FieldsFunc(in, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func fileBase(in string) string {
	words := splitIdentifier(in)
	if len(words) == 0 {
		return "generated"
	}
	for i := range words {
		words[i] = strings.ToLower(words[i])
	}
	return strings.Join(words, "_")
}

var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nwrfc-gen: "+format+"\n", args...)
	os.Exit(1)
}
