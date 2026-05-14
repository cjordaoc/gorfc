// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Command nwrfc-gen produces typed Go client code from a SAP
// RFC function descriptor.
//
// Tier 4.2 deliverable per docs/PLAN.md §10. Useful for teams
// that wrap a small set of BAPIs and want type-safe wrappers
// rather than the dynamic CallParams shape.
//
// Usage:
//
//	# Live SAP system: connect, describe, generate.
//	nwrfc-gen \
//	  --fn BAPI_USER_GET_DETAIL \
//	  --pkg bapiuser \
//	  --out internal/bapiuser/bapiuser.go
//
//	# Offline: read a saved descriptor JSON, generate.
//	nwrfc-gen --json bapi_user_get_detail.json --pkg bapiuser
//
//	# Emit the function descriptor JSON (with SourceNote)
//	# instead of generating code. The output round-trips
//	# back through --json.
//	nwrfc-gen --fn BAPI_USER_GET_DETAIL --describe
//
// The generated package exposes a typed In / Out struct pair
// per function, plus a Call helper that routes through
// nwrfc.Call. No runtime overhead beyond the marshaling layer
// the core library already pays.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
	fnName := flag.String("fn", "", "RFC function name (live mode)")
	jsonPath := flag.String("json", "", "descriptor JSON path (offline mode)")
	pkgName := flag.String("pkg", "rfcclient", "Go package name")
	outPath := flag.String("out", "", "output file path; default stdout")
	describeMode := flag.Bool("describe", false, "emit the function descriptor JSON instead of generating code")
	flag.Parse()

	var desc backend.FunctionDescriptor
	switch {
	case *jsonPath != "":
		var err error
		desc, err = loadDescriptor(*jsonPath)
		if err != nil {
			fail("%v", err)
		}
	case *fnName != "":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := nwrfc.Open(ctx, paramsFromEnv())
		if err != nil {
			fail("Open: %v", err)
		}
		defer conn.Close()
		desc, err = conn.Describe(ctx, *fnName)
		if err != nil {
			fail("Describe: %v", err)
		}
	default:
		flag.Usage()
		fail("--fn or --json required")
	}

	var src []byte
	var err error
	if *describeMode {
		src, err = writeDescriptorJSON(desc)
		if err != nil {
			fail("describe: %v", err)
		}
	} else {
		src, err = generate(*pkgName, desc)
		if err != nil {
			fail("generate: %v", err)
		}
	}
	if *outPath == "" {
		os.Stdout.Write(src)
		return
	}
	if err := os.WriteFile(*outPath, src, 0o644); err != nil {
		fail("write %s: %v", *outPath, err)
	}
}

// descriptorEnvelope wraps a [backend.FunctionDescriptor] with a
// SourceNote metadata field. It is the JSON shape emitted by
// "nwrfc-gen --describe" and matches the reference descriptors
// committed under descriptors/. The embedded descriptor's fields
// are promoted to the top level, so the envelope round-trips
// through loadDescriptor: a plain backend.FunctionDescriptor
// simply ignores the extra SourceNote key on unmarshal.
type descriptorEnvelope struct {
	SourceNote string `json:"SourceNote,omitempty"`
	backend.FunctionDescriptor
}

// sourceNote builds the stable, reproducible provenance string
// embedded in emitted descriptors. The text is deterministic for
// a given function name so regenerated descriptors stay
// byte-identical.
func sourceNote(fn string) string {
	return fmt.Sprintf("Reference descriptor for nexus-spec migration. "+
		"Regenerate with `nwrfc-gen describe --fn %s` against the target SAP system "+
		"before producing customer-specific generated code.", fn)
}

// writeDescriptorJSON serializes desc as a descriptorEnvelope with
// SourceNote populated, producing the canonical descriptor JSON
// shape consumed by loadDescriptor and the nexus-spec migration.
func writeDescriptorJSON(desc backend.FunctionDescriptor) ([]byte, error) {
	env := descriptorEnvelope{
		SourceNote:         sourceNote(desc.Name),
		FunctionDescriptor: desc,
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// loadDescriptor reads a descriptor JSON from path. Any SourceNote
// metadata key is ignored gracefully: backend.FunctionDescriptor
// has no such field, so JSON unmarshal drops the unknown key.
func loadDescriptor(path string) (backend.FunctionDescriptor, error) {
	var desc backend.FunctionDescriptor
	b, err := os.ReadFile(path)
	if err != nil {
		return desc, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &desc); err != nil {
		return desc, fmt.Errorf("parse %s: %w", path, err)
	}
	return desc, nil
}

func paramsFromEnv() nwrfc.Params {
	return nwrfc.Params{
		AsHost: os.Getenv("GORFC_TEST_ASHOST"),
		SysNr:  os.Getenv("GORFC_TEST_SYSNR"),
		Client: os.Getenv("GORFC_TEST_CLIENT"),
		User:   os.Getenv("GORFC_TEST_USER"),
		Passwd: os.Getenv("GORFC_TEST_PASSWD"),
		Lang:   os.Getenv("GORFC_TEST_LANG"),
	}
}

const fileTmpl = `// Code generated by nwrfc-gen. DO NOT EDIT.
// SPDX-License-Identifier: Apache-2.0

package {{.Package}}

import (
	"context"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// {{.GoName}} is the typed wrapper for the {{.RFCName}} RFC.
//
// Generated from descriptor:
//   parameters: {{range .Params}}{{.Name}} ({{.Direction}}); {{end}}

{{range .Structs}}
type {{.GoName}} struct {
{{- range .Fields}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
}
{{end}}

// {{.GoName}}In is the input parameters.
type {{.GoName}}In struct {
{{- range .Imports}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
}

// {{.GoName}}Out is the output parameters.
type {{.GoName}}Out struct {
{{- range .Exports}}
	{{.GoName}} {{.GoType}} ` + "`rfc:\"{{.RFCName}}\"`" + `
{{- end}}
}

// {{.GoName}} invokes the {{.RFCName}} RFC.
func {{.GoName}}(ctx context.Context, conn *nwrfc.Conn, in {{.GoName}}In) (out {{.GoName}}Out, raw backend.CallParams, err error) {
	raw, err = nwrfc.Call(ctx, conn, "{{.RFCName}}", in, &out)
	return
}
`

type tmplData struct {
	Package string
	GoName  string
	RFCName string
	Params  []paramDoc
	Imports []paramDoc
	Exports []paramDoc
	Structs []structDoc
}

type paramDoc struct {
	GoName    string
	RFCName   string
	GoType    string
	Direction string
}

type structDoc struct {
	GoName string
	Fields []paramDoc
}

func generate(pkg string, d backend.FunctionDescriptor) ([]byte, error) {
	data := tmplData{
		Package: pkg,
		GoName:  goIdentifier(d.Name),
		RFCName: d.Name,
	}
	seenStructs := map[string]bool{}
	for _, p := range d.Parameters {
		field := paramDoc{
			GoName:  goIdentifier(p.Name),
			RFCName: p.Name,
			GoType:  goTypeFor(p),
		}
		switch p.Direction {
		case backend.DirImport, backend.DirChanging:
			data.Imports = append(data.Imports, field)
		}
		switch p.Direction {
		case backend.DirExport, backend.DirChanging, backend.DirTables:
			data.Exports = append(data.Exports, field)
		}
		field.Direction = directionStr(p.Direction)
		data.Params = append(data.Params, field)
		// Collect referenced structs.
		if p.TypeDesc != nil && !seenStructs[p.TypeDesc.Name] {
			seenStructs[p.TypeDesc.Name] = true
			data.Structs = append(data.Structs, makeStructDoc(p.TypeDesc))
		}
	}

	t := template.Must(template.New("file").Parse(fileTmpl))
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

func makeStructDoc(td *backend.TypeDescriptor) structDoc {
	out := structDoc{GoName: goIdentifier(td.Name)}
	for _, f := range td.Fields {
		out.Fields = append(out.Fields, paramDoc{
			GoName:  goIdentifier(f.Name),
			RFCName: f.Name,
			GoType:  goTypeForField(f),
		})
	}
	return out
}

func goTypeFor(p backend.ParameterDescriptor) string {
	switch p.Type {
	case backend.TypeStructure:
		if p.TypeDesc != nil {
			return goIdentifier(p.TypeDesc.Name)
		}
		return "map[string]any"
	case backend.TypeTable:
		if p.TypeDesc != nil {
			return "[]" + goIdentifier(p.TypeDesc.Name)
		}
		return "[]any"
	}
	return goTypeForField(backend.FieldDescriptor{Type: p.Type, Length: p.Length})
}

func goTypeForField(f backend.FieldDescriptor) string {
	switch f.Type {
	case backend.TypeChar, backend.TypeNum, backend.TypeString,
		backend.TypeBCD, backend.TypeFloat, backend.TypeDecF16,
		backend.TypeDecF34, backend.TypeUTCLong:
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
	case backend.TypeDate:
		return "backend.Date"
	case backend.TypeTime:
		return "backend.Time"
	}
	return "any"
}

func directionStr(d backend.Direction) string {
	parts := []string{}
	if d&backend.DirImport != 0 {
		parts = append(parts, "IMPORT")
	}
	if d&backend.DirExport != 0 {
		parts = append(parts, "EXPORT")
	}
	if d&backend.DirChanging != 0 {
		parts = append(parts, "CHANGING")
	}
	if d&backend.DirTables != 0 {
		parts = append(parts, "TABLES")
	}
	if d&backend.DirReturn != 0 {
		parts = append(parts, "RETURN")
	}
	return strings.Join(parts, "|")
}

// goIdentifier converts an ABAP identifier (uppercase, may
// contain '/') into a valid CamelCase Go identifier.
func goIdentifier(in string) string {
	parts := strings.FieldsFunc(in, func(r rune) bool {
		return r == '_' || r == '/' || r == '-'
	})
	for i, p := range parts {
		parts[i] = strings.Title(strings.ToLower(p))
	}
	out := strings.Join(parts, "")
	if out == "" {
		out = "Generated"
	}
	return out
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nwrfc-gen: "+format+"\n", args...)
	os.Exit(1)
}
