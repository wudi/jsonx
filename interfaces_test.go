package jsonx

import (
	"bytes"
	encjson "encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ----------------------------------------------------------------------------
// Test types
// ----------------------------------------------------------------------------

// ptrMarshaler uses pointer-receiver methods on both sides. Most real-world
// Unmarshaler types look like this.
type ptrMarshaler struct {
	N int
}

func (p *ptrMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"ptr:%d"`, p.N)), nil
}

func (p *ptrMarshaler) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.TrimPrefix(s, `"ptr:`)
	s = strings.TrimSuffix(s, `"`)
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	p.N = n
	return nil
}

// valueMarshaler uses a value receiver for MarshalJSON. RawMessage is the
// canonical example in stdlib.
type valueMarshaler struct {
	S string
}

func (v valueMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"val:%s"`, v.S)), nil
}

func (v *valueMarshaler) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.TrimPrefix(s, `"val:`)
	s = strings.TrimSuffix(s, `"`)
	v.S = s
	return nil
}

// textOnly only implements TextMarshaler / TextUnmarshaler.
type textOnly struct {
	A, B int
}

func (t textOnly) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("%d-%d", t.A, t.B)), nil
}

func (t *textOnly) UnmarshalText(data []byte) error {
	parts := strings.Split(string(data), "-")
	if len(parts) != 2 {
		return fmt.Errorf("bad format: %q", data)
	}
	a, err := strconv.Atoi(parts[0])
	if err != nil {
		return err
	}
	b, err := strconv.Atoi(parts[1])
	if err != nil {
		return err
	}
	t.A, t.B = a, b
	return nil
}

type retainingTextUnmarshaler struct {
	Raw []byte
}

func (r *retainingTextUnmarshaler) UnmarshalText(data []byte) error {
	r.Raw = data
	return nil
}

// erroringMarshaler returns an error from MarshalJSON.
type erroringMarshaler struct{}

var errMarshal = errors.New("cannot marshal")

func (erroringMarshaler) MarshalJSON() ([]byte, error) { return nil, errMarshal }

// invalidMarshaler returns syntactically invalid JSON.
type invalidMarshaler struct{}

func (invalidMarshaler) MarshalJSON() ([]byte, error) { return []byte(`{not json`), nil }

// erroringUnmarshaler returns an error from UnmarshalJSON.
type erroringUnmarshaler struct{}

var errUnmarshal = errors.New("cannot unmarshal")

func (*erroringUnmarshaler) UnmarshalJSON([]byte) error { return errUnmarshal }

// nullAware treats null specially and records the fact.
type nullAware struct {
	WasNull bool
	Val     int
}

func (n *nullAware) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		n.WasNull = true
		return nil
	}
	return encjson.Unmarshal(data, &n.Val)
}

// counterUnmarshaler tracks how many times UnmarshalJSON was invoked on it
// and what bytes it saw — verifies we hand over the raw value unmodified.
type counterUnmarshaler struct {
	Calls int
	Last  string
}

func (c *counterUnmarshaler) UnmarshalJSON(data []byte) error {
	c.Calls++
	c.Last = string(data)
	return nil
}

type retainingUnmarshaler struct {
	Raw []byte
}

func (r *retainingUnmarshaler) UnmarshalJSON(data []byte) error {
	r.Raw = data
	return nil
}

// ----------------------------------------------------------------------------
// Marshaler encode
// ----------------------------------------------------------------------------

func TestMarshaler_PtrReceiver_TopLevel(t *testing.T) {
	v := &ptrMarshaler{N: 7}
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"ptr:7"` {
		t.Fatalf("got %s", got)
	}
}

func TestMarshaler_PtrReceiver_NonPtrValue(t *testing.T) {
	// Value passed by value — must still dispatch via pointer-receiver method
	// because jsonx makes the value addressable (heap copy) before calling.
	v := ptrMarshaler{N: 11}
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"ptr:11"` {
		t.Fatalf("got %s", got)
	}
}

func TestMarshaler_ValueReceiver(t *testing.T) {
	v := valueMarshaler{S: "abc"}
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"val:abc"` {
		t.Fatalf("got %s", got)
	}
}

func TestMarshaler_NilPtr_TopLevel(t *testing.T) {
	var v *ptrMarshaler
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `null` {
		t.Fatalf("got %s", got)
	}
}

func TestMarshaler_Error(t *testing.T) {
	_, err := Marshal(erroringMarshaler{})
	if err == nil {
		t.Fatal("expected error")
	}
	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("want *MarshalerError, got %T", err)
	}
	if !errors.Is(err, errMarshal) {
		t.Fatalf("Unwrap chain broken: %v", err)
	}
}

func TestMarshaler_InvalidJSONReturned(t *testing.T) {
	_, err := Marshal(invalidMarshaler{})
	if err == nil {
		t.Fatal("expected error")
	}
	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("want *MarshalerError, got %T", err)
	}
}

// ----------------------------------------------------------------------------
// Unmarshaler decode
// ----------------------------------------------------------------------------

func TestUnmarshaler_PtrReceiver_TopLevel(t *testing.T) {
	var v ptrMarshaler
	if err := Unmarshal([]byte(`"ptr:42"`), &v); err != nil {
		t.Fatal(err)
	}
	if v.N != 42 {
		t.Fatalf("got %+v", v)
	}
}

func TestUnmarshaler_PtrTargetAllocated(t *testing.T) {
	var v *ptrMarshaler
	if err := Unmarshal([]byte(`"ptr:99"`), &v); err != nil {
		t.Fatal(err)
	}
	if v == nil || v.N != 99 {
		t.Fatalf("got %+v", v)
	}
}

func TestUnmarshaler_NullOnPtrSetsNil(t *testing.T) {
	// When the target is *T, `null` must leave the pointer nil — we must NOT
	// call UnmarshalJSON on an allocated zero.
	pre := &ptrMarshaler{N: 5}
	v := pre
	if err := Unmarshal([]byte(`null`), &v); err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("expected nil, got %+v", v)
	}
}

func TestUnmarshaler_NullOnValue_PassesNull(t *testing.T) {
	// When the target is T (not a pointer), null is routed to UnmarshalJSON
	// unchanged. nullAware observes it.
	var v nullAware
	if err := Unmarshal([]byte(`null`), &v); err != nil {
		t.Fatal(err)
	}
	if !v.WasNull {
		t.Fatalf("UnmarshalJSON not called with null")
	}
}

func TestUnmarshaler_Error(t *testing.T) {
	var v erroringUnmarshaler
	err := Unmarshal([]byte(`{"x":1}`), &v)
	if !errors.Is(err, errUnmarshal) {
		t.Fatalf("want errUnmarshal, got %v", err)
	}
}

func TestUnmarshaler_RawBytesArePreserved(t *testing.T) {
	// The bytes handed to UnmarshalJSON must be the exact raw JSON value
	// (including whitespace inside objects, since we don't compact).
	var c counterUnmarshaler
	src := []byte(`{ "a" :  1 ,  "b":[1,2]}`)
	if err := Unmarshal(src, &c); err != nil {
		t.Fatal(err)
	}
	if c.Calls != 1 {
		t.Fatalf("calls = %d", c.Calls)
	}
	if c.Last != `{ "a" :  1 ,  "b":[1,2]}` {
		t.Fatalf("raw mismatch: %q", c.Last)
	}
}

func TestUnmarshaler_RetainedRawBytesFollowStdlibContract(t *testing.T) {
	// encoding/json documents that UnmarshalJSON implementations must copy
	// data if they retain it after returning; jsonx should not pre-copy every
	// raw value on their behalf.
	src := []byte(`{"a":1}`)
	var r retainingUnmarshaler
	if err := Unmarshal(src, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Raw) == 0 || r.Raw[0] != '{' {
		t.Fatalf("raw bytes = %q", r.Raw)
	}
	src[0] = 'X'
	if r.Raw[0] != 'X' {
		t.Fatalf("raw bytes were eagerly copied: %q", r.Raw)
	}
}

func TestUnmarshaler_AllJSONValueShapes(t *testing.T) {
	// Cover every scalar/composite JSON value shape so we know skipValue
	// captures the whole thing for each kind.
	cases := []string{
		`null`,
		`true`,
		`false`,
		`42`,
		`-3.14e2`,
		`"hello\nworld"`,
		`[1, 2, [3, [4]]]`,
		`{"k":"v","nested":{"a":[1,2,3]}}`,
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			var cu counterUnmarshaler
			if err := Unmarshal([]byte(c), &cu); err != nil {
				t.Fatal(err)
			}
			if cu.Last != c {
				t.Fatalf("raw mismatch:\n got  %q\n want %q", cu.Last, c)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// TextMarshaler / TextUnmarshaler
// ----------------------------------------------------------------------------

func TestTextMarshaler_TopLevel(t *testing.T) {
	v := textOnly{A: 1, B: 2}
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"1-2"` {
		t.Fatalf("got %s", got)
	}
}

func TestTextUnmarshaler_TopLevel(t *testing.T) {
	var v textOnly
	if err := Unmarshal([]byte(`"5-9"`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 5 || v.B != 9 {
		t.Fatalf("got %+v", v)
	}
}

func TestTextUnmarshaler_RetainedBytesFollowStdlibContract(t *testing.T) {
	src := []byte(`"abc"`)
	var v retainingTextUnmarshaler
	if err := Unmarshal(src, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Raw) == 0 || v.Raw[0] != 'a' {
		t.Fatalf("raw bytes = %q", v.Raw)
	}
	src[1] = 'X'
	if v.Raw[0] != 'X' {
		t.Fatalf("text bytes were eagerly copied: %q", v.Raw)
	}
}

func TestTextUnmarshaler_RejectsNonString(t *testing.T) {
	var v textOnly
	err := Unmarshal([]byte(`42`), &v)
	if err == nil {
		t.Fatal("expected error")
	}
	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("want *UnmarshalTypeError, got %T: %v", err, err)
	}
}

func TestTextUnmarshaler_NullIsNoop(t *testing.T) {
	// stdlib: null on a TextUnmarshaler value-target leaves the value alone
	// (UnmarshalText is not called). Verify parity.
	v := textOnly{A: 1, B: 2}
	if err := Unmarshal([]byte(`null`), &v); err != nil {
		t.Fatal(err)
	}
	var std textOnly = textOnly{A: 1, B: 2}
	if err := encjson.Unmarshal([]byte(`null`), &std); err != nil {
		t.Fatal(err)
	}
	if v != std {
		t.Fatalf("null-on-TextUnmarshaler divergence: ours=%+v stdlib=%+v", v, std)
	}
}

func BenchmarkTextUnmarshalerDecode_Jsonx(b *testing.B) {
	data := []byte(`"123-456"`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v textOnly
		if err := Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTextUnmarshalerDecode_Stdlib(b *testing.B) {
	data := []byte(`"123-456"`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v textOnly
		if err := encjson.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}

// ----------------------------------------------------------------------------
// json.RawMessage
// ----------------------------------------------------------------------------

func TestRawMessage_DecodeTopLevel(t *testing.T) {
	src := []byte(`{"a":1,"b":[2,3]}`)
	var raw encjson.RawMessage
	if err := Unmarshal(src, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(src) {
		t.Fatalf("got %q", raw)
	}
}

func TestRawMessage_EncodeTopLevel(t *testing.T) {
	raw := encjson.RawMessage(`{"a":1}`)
	got, err := Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("got %s", got)
	}
}

func TestRawMessage_InStruct(t *testing.T) {
	type Outer struct {
		Name string             `json:"name"`
		Raw  encjson.RawMessage `json:"raw"`
	}
	src := []byte(`{"name":"x","raw":{"inner":[1,2,3]}}`)
	var o Outer
	if err := Unmarshal(src, &o); err != nil {
		t.Fatal(err)
	}
	if o.Name != "x" {
		t.Fatalf("Name %q", o.Name)
	}
	if string(o.Raw) != `{"inner":[1,2,3]}` {
		t.Fatalf("Raw %q", o.Raw)
	}

	// Round-trip
	out, err := Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var back Outer
	if err := Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != o.Name || string(back.Raw) != string(o.Raw) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", back, o)
	}
}

func TestRawMessage_InSlice(t *testing.T) {
	src := []byte(`[1,"two",{"k":3},[4]]`)
	var arr []encjson.RawMessage
	if err := Unmarshal(src, &arr); err != nil {
		t.Fatal(err)
	}
	want := []string{`1`, `"two"`, `{"k":3}`, `[4]`}
	if len(arr) != len(want) {
		t.Fatalf("len %d", len(arr))
	}
	for i := range arr {
		if string(arr[i]) != want[i] {
			t.Fatalf("[%d] = %q, want %q", i, arr[i], want[i])
		}
	}
}

func TestRawMessage_InMap(t *testing.T) {
	src := []byte(`{"a":1,"b":"two","c":{"k":3}}`)
	var m map[string]encjson.RawMessage
	if err := Unmarshal(src, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "1", "b": `"two"`, "c": `{"k":3}`}
	if len(m) != len(want) {
		t.Fatalf("len %d", len(m))
	}
	for k, v := range want {
		if string(m[k]) != v {
			t.Fatalf("%s = %q, want %q", k, m[k], v)
		}
	}
}

func TestRawMessage_NilEncodesAsNull(t *testing.T) {
	var raw encjson.RawMessage
	got, err := Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	// stdlib: nil RawMessage → "null"
	if string(got) != `null` {
		t.Fatalf("got %s", got)
	}
}

// ----------------------------------------------------------------------------
// Nested / compound types
// ----------------------------------------------------------------------------

func TestStructWithMarshalerField(t *testing.T) {
	type Outer struct {
		Name string        `json:"name"`
		P    *ptrMarshaler `json:"p"`
	}
	src := []byte(`{"name":"hi","p":"ptr:17"}`)
	var o Outer
	if err := Unmarshal(src, &o); err != nil {
		t.Fatal(err)
	}
	if o.Name != "hi" || o.P == nil || o.P.N != 17 {
		t.Fatalf("got %+v / %+v", o, o.P)
	}
	out, err := Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"name":"hi","p":"ptr:17"}` {
		t.Fatalf("got %s", out)
	}
}

func TestStructWithMarshalerField_NilPtr(t *testing.T) {
	type Outer struct {
		P *ptrMarshaler `json:"p"`
	}
	o := Outer{P: nil}
	out, err := Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"p":null}` {
		t.Fatalf("got %s", out)
	}
	var back Outer
	if err := Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.P != nil {
		t.Fatalf("expected nil, got %+v", back.P)
	}
}

func TestSliceOfMarshalers(t *testing.T) {
	v := []ptrMarshaler{{N: 1}, {N: 2}, {N: 3}}
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `["ptr:1","ptr:2","ptr:3"]` {
		t.Fatalf("got %s", got)
	}
	var back []ptrMarshaler
	if err := Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, back) {
		t.Fatalf("round-trip: got %+v", back)
	}
}

func TestMapValueMarshaler(t *testing.T) {
	m := map[string]ptrMarshaler{"a": {N: 10}, "b": {N: 20}}
	got, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	// Map iteration order is random; parse the output and check each key.
	var back map[string]encjson.RawMessage
	if err := Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if string(back["a"]) != `"ptr:10"` || string(back["b"]) != `"ptr:20"` {
		t.Fatalf("got %s", got)
	}

	// Round-trip through decode.
	var decoded map[string]ptrMarshaler
	if err := Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m, decoded) {
		t.Fatalf("round-trip: got %+v", decoded)
	}
}

func TestNestedMarshaler(t *testing.T) {
	type Inner struct {
		P ptrMarshaler `json:"p"`
	}
	type Outer struct {
		I Inner `json:"i"`
	}
	o := Outer{I: Inner{P: ptrMarshaler{N: 41}}}
	got, err := Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"i":{"p":"ptr:41"}}` {
		t.Fatalf("got %s", got)
	}
	var back Outer
	if err := Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if back.I.P.N != 41 {
		t.Fatalf("round-trip: got %+v", back)
	}
}

// ----------------------------------------------------------------------------
// encoder interface{}-value paths exercising Marshaler dispatch
// ----------------------------------------------------------------------------

func TestMarshaler_InsideInterfaceMap(t *testing.T) {
	m := map[string]interface{}{
		"raw": encjson.RawMessage(`{"x":1}`),
	}
	got, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"raw":{"x":1}}` {
		t.Fatalf("got %s", got)
	}
}

func TestMarshaler_InsideInterfaceSlice(t *testing.T) {
	v := []interface{}{
		encjson.RawMessage(`1`),
		encjson.RawMessage(`"hi"`),
		encjson.RawMessage(`[true]`),
	}
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `[1,"hi",[true]]` {
		t.Fatalf("got %s", got)
	}
}

// ----------------------------------------------------------------------------
// Parity with encoding/json — real-world Marshaler/Unmarshaler types
// ----------------------------------------------------------------------------

func TestParity_TimeMarshaler(t *testing.T) {
	// time.Time implements json.Marshaler (RFC3339Nano), so the Marshaler
	// path is exercised. Validate byte-exact parity with stdlib.
	ts := time.Date(2024, 3, 14, 15, 9, 26, 535897932, time.UTC)
	ours, err := Marshal(ts)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := encjson.Marshal(ts)
	if err != nil {
		t.Fatal(err)
	}
	if string(ours) != string(theirs) {
		t.Fatalf("marshal mismatch:\n  ours:   %s\n  stdlib: %s", ours, theirs)
	}

	// Unmarshal parity.
	var a, b time.Time
	if err := Unmarshal(ours, &a); err != nil {
		t.Fatal(err)
	}
	if err := encjson.Unmarshal(ours, &b); err != nil {
		t.Fatal(err)
	}
	if !a.Equal(b) {
		t.Fatalf("time unmarshal differs: ours=%v stdlib=%v", a, b)
	}
}

func TestParity_BigInt(t *testing.T) {
	// *big.Int implements json.Marshaler (value is an absurdly large integer
	// that overflows int64).
	n, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	ours, err := Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := encjson.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if string(ours) != string(theirs) {
		t.Fatalf("marshal mismatch:\n  ours:   %s\n  stdlib: %s", ours, theirs)
	}
}

func TestParity_ComplexStruct(t *testing.T) {
	type X struct {
		When time.Time          `json:"when"`
		Raw  encjson.RawMessage `json:"raw"`
		P    *ptrMarshaler      `json:"p"`
	}
	x := X{
		When: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Raw:  encjson.RawMessage(`[1,2,3]`),
		P:    &ptrMarshaler{N: 99},
	}
	ours, err := Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	// Key order between encoders is identical (both use field declaration
	// order), so we can compare byte-for-byte with stdlib.
	theirs, err := encjson.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	if string(ours) != string(theirs) {
		t.Fatalf("marshal mismatch:\n  ours:   %s\n  stdlib: %s", ours, theirs)
	}

	var ourBack, stdBack X
	if err := Unmarshal(ours, &ourBack); err != nil {
		t.Fatal(err)
	}
	if err := encjson.Unmarshal(ours, &stdBack); err != nil {
		t.Fatal(err)
	}
	if !ourBack.When.Equal(stdBack.When) {
		t.Fatalf("time differs: ours=%v stdlib=%v", ourBack.When, stdBack.When)
	}
	if string(ourBack.Raw) != string(stdBack.Raw) {
		t.Fatalf("raw differs")
	}
	if ourBack.P.N != stdBack.P.N {
		t.Fatalf("ptrMarshaler differs")
	}
}
