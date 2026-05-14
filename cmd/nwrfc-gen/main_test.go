// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
)

func sampleDescriptor() backend.FunctionDescriptor {
	return backend.FunctionDescriptor{
		Name: "BAPI_USER_GET_DETAIL",
		Parameters: []backend.ParameterDescriptor{
			{
				Name:      "USERNAME",
				Type:      backend.TypeChar,
				Direction: backend.DirImport,
				Length:    12,
			},
			{
				Name:      "RETURN",
				Type:      backend.TypeTable,
				Direction: backend.DirTables,
			},
		},
	}
}

func TestWriteDescriptorJSONEmitsSourceNote(t *testing.T) {
	desc := sampleDescriptor()
	b, err := writeDescriptorJSON(desc)
	if err != nil {
		t.Fatalf("writeDescriptorJSON: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("emitted JSON is not an object: %v", err)
	}
	note, ok := raw["SourceNote"]
	if !ok {
		t.Fatal("emitted JSON has no SourceNote field")
	}
	var noteStr string
	if err := json.Unmarshal(note, &noteStr); err != nil {
		t.Fatalf("SourceNote is not a string: %v", err)
	}
	if !strings.Contains(noteStr, desc.Name) {
		t.Errorf("SourceNote %q does not contain function name %q", noteStr, desc.Name)
	}

	// The descriptor fields are promoted to the top level so the
	// shape matches the committed descriptors/ artifacts.
	if _, ok := raw["Name"]; !ok {
		t.Error("emitted JSON has no promoted Name field")
	}
	if _, ok := raw["Parameters"]; !ok {
		t.Error("emitted JSON has no promoted Parameters field")
	}
}

func TestWriteDescriptorJSONIsDeterministic(t *testing.T) {
	desc := sampleDescriptor()
	a, err := writeDescriptorJSON(desc)
	if err != nil {
		t.Fatalf("writeDescriptorJSON: %v", err)
	}
	b, err := writeDescriptorJSON(desc)
	if err != nil {
		t.Fatalf("writeDescriptorJSON: %v", err)
	}
	if string(a) != string(b) {
		t.Error("writeDescriptorJSON output is not reproducible across calls")
	}
}

func TestDescriptorJSONRoundTrips(t *testing.T) {
	desc := sampleDescriptor()
	b, err := writeDescriptorJSON(desc)
	if err != nil {
		t.Fatalf("writeDescriptorJSON: %v", err)
	}

	path := filepath.Join(t.TempDir(), "descriptor.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write temp descriptor: %v", err)
	}

	got, err := loadDescriptor(path)
	if err != nil {
		t.Fatalf("loadDescriptor: %v", err)
	}
	// loadDescriptor must drop the SourceNote key gracefully and
	// reproduce the original descriptor exactly.
	if !reflect.DeepEqual(got, desc) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, desc)
	}
}
