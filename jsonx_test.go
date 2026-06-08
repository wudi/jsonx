package jsonx

import (
	encjson "encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type testUser struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Age       int      `json:"age"`
	Active    bool     `json:"active"`
	Score     float64  `json:"score"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
}

func TestUnmarshalStruct(t *testing.T) {
	data := []byte(`{"id":42,"name":"Alice","email":"a@b","age":30,"active":true,"score":98.5,"tags":["x","y"],"created_at":"t"}`)
	var got testUser
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := testUser{42, "Alice", "a@b", 30, true, 98.5, []string{"x", "y"}, "t"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestUnmarshalInterface(t *testing.T) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			var ours, stdlib interface{}
			if err := Unmarshal(data, &ours); err != nil {
				t.Fatalf("jsonx err: %v", err)
			}
			if err := encjson.Unmarshal(data, &stdlib); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ours, stdlib) {
				t.Fatalf("%s: decode mismatch vs stdlib", name)
			}
		})
	}
}

func TestUnmarshalByteSliceAvoidsBase64SourceCopy(t *testing.T) {
	data := []byte(`"SGVsbG8sIHdvcmxkIQ=="`)
	var warm []byte
	if err := Unmarshal(data, &warm); err != nil {
		t.Fatal(err)
	}
	if string(warm) != "Hello, world!" {
		t.Fatalf("decoded %q", warm)
	}

	var last []byte
	var err error
	allocs := testing.AllocsPerRun(1000, func() {
		var got []byte
		err = Unmarshal(data, &got)
		last = got
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(last) != "Hello, world!" {
		t.Fatalf("decoded %q", last)
	}
	if allocs > 2 {
		t.Fatalf("allocs = %.0f, want <= 2", allocs)
	}
}

func TestUnmarshalEscapedByteSliceAvoidsStringCopy(t *testing.T) {
	data := []byte(`"\u0053GVsbG8sIHdvcmxkIQ=="`)
	var warm []byte
	if err := Unmarshal(data, &warm); err != nil {
		t.Fatal(err)
	}
	if string(warm) != "Hello, world!" {
		t.Fatalf("decoded %q", warm)
	}

	var last []byte
	var err error
	allocs := testing.AllocsPerRun(1000, func() {
		var got []byte
		err = Unmarshal(data, &got)
		last = got
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(last) != "Hello, world!" {
		t.Fatalf("decoded %q", last)
	}
	if allocs > 2 {
		t.Fatalf("allocs = %.0f, want <= 2", allocs)
	}
}

func BenchmarkUnmarshalByteSlice_Jsonx(b *testing.B) {
	data := []byte(`"SGVsbG8sIHdvcmxkIQ=="`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got []byte
		if err := Unmarshal(data, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalByteSlice_Stdlib(b *testing.B) {
	data := []byte(`"SGVsbG8sIHdvcmxkIQ=="`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got []byte
		if err := encjson.Unmarshal(data, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalEscapedByteSlice_Jsonx(b *testing.B) {
	data := []byte(`"\u0053GVsbG8sIHdvcmxkIQ=="`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got []byte
		if err := Unmarshal(data, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalEscapedByteSlice_Stdlib(b *testing.B) {
	data := []byte(`"\u0053GVsbG8sIHdvcmxkIQ=="`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got []byte
		if err := encjson.Unmarshal(data, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMarshalByteSliceAvoidsBase64TempSlice(t *testing.T) {
	v := []byte("Hello, world!")
	warm, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(warm) != `"SGVsbG8sIHdvcmxkIQ=="` {
		t.Fatalf("encoded %q", warm)
	}

	var out []byte
	allocs := testing.AllocsPerRun(1000, func() {
		out, err = Marshal(v)
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"SGVsbG8sIHdvcmxkIQ=="` {
		t.Fatalf("encoded %q", out)
	}
	if allocs > 2 {
		t.Fatalf("allocs = %.0f, want <= 2", allocs)
	}
}

func BenchmarkMarshalByteSlice_Jsonx(b *testing.B) {
	v := []byte("Hello, world!")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalByteSlice_Stdlib(b *testing.B) {
	v := []byte("Hello, world!")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := encjson.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

// TestValid mirrors encoding/json's TestValid (scanner_test.go): a hand-picked
// table of valid/invalid inputs. We then cross-check every case against the
// stdlib so divergence (in either direction) is a test failure.
func TestValid(t *testing.T) {
	tests := []struct {
		name string
		data string
		ok   bool
	}{
		// from stdlib scanner_test.go
		{"bare_ident", `foo`, false},
		{"swapped_braces", `}{`, false},
		{"mixed_brackets", `{]`, false},
		{"empty_obj", `{}`, true},
		{"simple_obj", `{"foo":"bar"}`, true},
		{"nested_obj", `{"foo":"bar","bar":{"baz":["qux"]}}`, true},

		// scalars
		{"true", `true`, true},
		{"false", `false`, true},
		{"null", `null`, true},
		{"zero", `0`, true},
		{"negzero", `-0`, true},
		{"int", `42`, true},
		{"negint", `-42`, true},
		{"frac", `3.14`, true},
		{"exp", `1e10`, true},
		{"expneg", `1e-10`, true},
		{"expsign", `1E+10`, true},
		{"fracExp", `-1.5e+3`, true},
		{"emptyStr", `""`, true},
		{"str", `"hello"`, true},
		{"strEsc", `"a\"b\\c\/d\b\f\n\r\t"`, true},
		{"strU", `"\u0041"`, true},
		{"strSurrogate", `"\uD83D\uDE00"`, true},

		// whitespace
		{"ws_leading", "   true", true},
		{"ws_trailing", "true   ", true},
		{"ws_both", " \n\r\t42\t\n", true},
		{"ws_only", `   `, false},
		{"empty", ``, false},

		// arrays / objects
		{"empty_arr", `[]`, true},
		{"arr_scalars", `[1,2,3]`, true},
		{"arr_mixed", `[1,"x",true,null,{}]`, true},
		{"nested_arr", `[[[[[]]]]]`, true},
		{"obj_nested_ws", "{ \"a\" : [ 1 , 2 ] , \"b\" : { } }", true},

		// invalid numbers
		{"bad_plus", `+1`, false},
		{"bad_dot", `.5`, false},
		{"bad_trailing_dot", `1.`, false},
		{"bad_leading_zero", `01`, false},
		{"bad_neg_leading_zero", `-01`, false},
		{"bad_exp_empty", `1e`, false},
		{"bad_exp_sign_only", `1e+`, false},
		{"bare_minus", `-`, false},
		{"hex", `0x1`, false},

		// invalid strings
		{"unterminated_str", `"abc`, false},
		{"bad_escape", `"\x"`, false},
		{"bad_uN_short", `"\u00"`, false},
		{"bad_uN_nonhex", `"\u00GG"`, false},
		{"raw_ctrl", "\"\x01\"", false},
		{"raw_tab", "\"\t\"", false},

		// invalid literals
		{"nul", `nul`, false},
		{"tru", `tru`, false},
		{"False_case", `False`, false},
		{"Null_case", `Null`, false},

		// structural errors
		{"unclosed_obj", `{`, false},
		{"unclosed_arr", `[`, false},
		{"trailing_comma_arr", `[1,2,]`, false},
		{"trailing_comma_obj", `{"a":1,}`, false},
		{"missing_colon", `{"a" 1}`, false},
		{"missing_comma_arr", `[1 2]`, false},
		{"missing_comma_obj", `{"a":1 "b":2}`, false},
		{"double_comma", `[1,,2]`, false},
		{"unquoted_key", `{a:1}`, false},
		{"single_quoted", `'x'`, false},

		// trailing garbage
		{"trailing_junk_scalar", `42x`, false},
		{"trailing_junk_arr", `[1,2,3]x`, false},
		{"trailing_second_value", `1 2`, false},
		{"multi_objs", `{}{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Valid([]byte(tt.data))
			if got != tt.ok {
				t.Errorf("Valid(%q) = %v, want %v", tt.data, got, tt.ok)
			}
			if std := encjson.Valid([]byte(tt.data)); std != tt.ok {
				t.Errorf("stdlib.Valid(%q) = %v, want %v (table mismatch with stdlib)", tt.data, std, tt.ok)
			}
		})
	}
}

// TestValidCorpus cross-checks Valid() against encoding/json.Valid() over the
// bundled testdata. Every file must be valid in both, and a surgically
// corrupted variant must be invalid in both.
func TestValidCorpus(t *testing.T) {
	files := []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			if !Valid(data) {
				t.Fatalf("%s: Valid returned false on valid input", name)
			}
			if !encjson.Valid(data) {
				t.Fatalf("%s: stdlib rejects input — corrupt testdata?", name)
			}

			// truncations must behave the same as stdlib (trailing-WS cuts can
			// still parse; the point is parity, not that truncation => invalid).
			for _, cut := range []int{1, len(data) / 4, len(data) / 2, 3 * len(data) / 4} {
				if cut <= 0 || cut >= len(data) {
					continue
				}
				trunc := data[:cut]
				if got, want := Valid(trunc), encjson.Valid(trunc); got != want {
					t.Errorf("%s: Valid(trunc@%d)=%v, stdlib=%v", name, cut, got, want)
				}
			}

			// trailing garbage must be invalid
			garbled := append(append([]byte{}, data...), 'x')
			if Valid(garbled) {
				t.Errorf("%s: Valid accepted trailing garbage", name)
			}
		})
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	for _, name := range []string{"small.json", "twitter.json", "citm_catalog.json", "canada.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			var v interface{}
			if err := Unmarshal(data, &v); err != nil {
				t.Fatal(err)
			}
			out, err := Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			// re-parse and compare
			var rt interface{}
			if err := encjson.Unmarshal(out, &rt); err != nil {
				t.Fatalf("stdlib cannot parse our output: %v", err)
			}
			if !reflect.DeepEqual(v, rt) {
				t.Fatalf("roundtrip mismatch")
			}
		})
	}
}
