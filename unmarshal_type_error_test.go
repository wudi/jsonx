package jsonx

import (
	"errors"
	"strings"
	"testing"
)

// Reproduces the case from GitHub issue #1: an array value into a string
// field should surface as *UnmarshalTypeError with the field path filled in,
// matching encoding/json's behavior (and replacing the prior misleading
// "expected string at offset 12" syntax error).
func TestIssue1_TypeMismatchReportsStructField(t *testing.T) {
	type errResp struct {
		CoverURL string
	}
	raw := []byte(`{"CoverURL":[{"Height":720, "Width":720}]}`)

	err := Unmarshal(raw, &errResp{})
	if err == nil {
		t.Fatal("expected an error")
	}

	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
	}
	if ute.Value != "array" {
		t.Errorf("Value: got %q, want %q", ute.Value, "array")
	}
	if ute.Type == nil || ute.Type.String() != "string" {
		t.Errorf("Type: got %v, want string", ute.Type)
	}
	if ute.Struct != "errResp" {
		t.Errorf("Struct: got %q, want %q", ute.Struct, "errResp")
	}
	if ute.Field != "CoverURL" {
		t.Errorf("Field: got %q, want %q", ute.Field, "CoverURL")
	}
	want := "jsonx: cannot unmarshal array into Go struct field errResp.CoverURL of type string"
	if got := err.Error(); got != want {
		t.Errorf("Error():\n got  %q\n want %q", got, want)
	}
}

// Nested struct field path: outer struct decoder must extend the field
// path returned by the inner struct decoder (Outer.Inner.Name), and Struct
// must be the outermost type's name.
func TestUnmarshalTypeError_NestedStructFieldPath(t *testing.T) {
	type Inner struct {
		Name string
	}
	type Outer struct {
		Inner Inner
	}

	err := Unmarshal([]byte(`{"Inner":{"Name":[]}}`), &Outer{})
	if err == nil {
		t.Fatal("expected error")
	}
	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
	}
	if ute.Struct != "Outer" {
		t.Errorf("Struct: got %q, want %q", ute.Struct, "Outer")
	}
	if ute.Field != "Inner.Name" {
		t.Errorf("Field: got %q, want %q", ute.Field, "Inner.Name")
	}
	if !strings.Contains(err.Error(), "Outer.Inner.Name") {
		t.Errorf("Error() missing dotted path: %s", err.Error())
	}
}

// Type-mismatch reporting for each scalar Kind. Each row drops a
// well-formed JSON value of the wrong type into a struct field; the
// reported Value must classify the JSON value the user actually wrote.
func TestUnmarshalTypeError_PerKind(t *testing.T) {
	type S struct {
		B bool
		I int
		U uint
		F float64
		S string
	}
	cases := []struct {
		name    string
		input   string
		field   string
		value   string // expected UnmarshalTypeError.Value
		typeStr string // expected UnmarshalTypeError.Type.String()
	}{
		{"array_into_bool", `{"B":[]}`, "B", "array", "bool"},
		{"object_into_int", `{"I":{}}`, "I", "object", "int"},
		{"string_into_uint", `{"U":"x"}`, "U", "string", "uint"},
		{"bool_into_float", `{"F":true}`, "F", "bool", "float64"},
		{"number_into_string", `{"S":42}`, "S", "number", "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v S
			err := Unmarshal([]byte(tc.input), &v)
			if err == nil {
				t.Fatal("expected error")
			}
			var ute *UnmarshalTypeError
			if !errors.As(err, &ute) {
				t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
			}
			if ute.Value != tc.value {
				t.Errorf("Value: got %q, want %q", ute.Value, tc.value)
			}
			if ute.Type == nil || ute.Type.String() != tc.typeStr {
				t.Errorf("Type: got %v, want %s", ute.Type, tc.typeStr)
			}
			if ute.Struct != "S" {
				t.Errorf("Struct: got %q, want %q", ute.Struct, "S")
			}
			if ute.Field != tc.field {
				t.Errorf("Field: got %q, want %q", ute.Field, tc.field)
			}
		})
	}
}

// Pointer field: the reported Type should be the *underlying* type
// (matching encoding/json), since the pointer is transparently dereferenced.
func TestUnmarshalTypeError_PointerField(t *testing.T) {
	type S struct {
		P *string
	}
	err := Unmarshal([]byte(`{"P":[]}`), &S{})
	if err == nil {
		t.Fatal("expected error")
	}
	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
	}
	if ute.Type == nil || ute.Type.String() != "string" {
		t.Errorf("Type: got %v, want string", ute.Type)
	}
	if ute.Field != "P" {
		t.Errorf("Field: got %q, want %q", ute.Field, "P")
	}
}

// Slice-element mismatch under a struct field: the field name still
// appears in the path, the Type is the element type.
func TestUnmarshalTypeError_SliceElementUnderStructField(t *testing.T) {
	type S struct {
		Nums []int
	}
	err := Unmarshal([]byte(`{"Nums":[true]}`), &S{})
	if err == nil {
		t.Fatal("expected error")
	}
	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
	}
	if ute.Value != "bool" {
		t.Errorf("Value: got %q, want %q", ute.Value, "bool")
	}
	if ute.Type == nil || ute.Type.String() != "int" {
		t.Errorf("Type: got %v, want int", ute.Type)
	}
	if ute.Field != "Nums" {
		t.Errorf("Field: got %q, want %q", ute.Field, "Nums")
	}
}

// Top-level non-array fed into a slice target: existing slice-decoder
// branch now classifies the offending value rather than reporting
// "non-array".
func TestUnmarshalTypeError_TopLevelSliceTypeMismatch(t *testing.T) {
	var s []int
	err := Unmarshal([]byte(`"foo"`), &s)
	if err == nil {
		t.Fatal("expected error")
	}
	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
	}
	if ute.Value != "string" {
		t.Errorf("Value: got %q, want %q", ute.Value, "string")
	}
	if ute.Type == nil || ute.Type.String() != "[]int" {
		t.Errorf("Type: got %v, want []int", ute.Type)
	}
}

// Top-level scalar mismatch (no enclosing struct) should still produce a
// *UnmarshalTypeError with Type set, and Error() should use the "Go value
// of type T" phrasing instead of the struct-field phrasing.
func TestUnmarshalTypeError_TopLevelScalar(t *testing.T) {
	var s string
	err := Unmarshal([]byte(`[]`), &s)
	if err == nil {
		t.Fatal("expected error")
	}
	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
	}
	want := "jsonx: cannot unmarshal array into Go value of type string"
	if got := err.Error(); got != want {
		t.Errorf("Error():\n got  %q\n want %q", got, want)
	}
}
