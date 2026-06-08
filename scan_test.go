package jsonx

import (
	"strings"
	"testing"
	"unsafe"
)

func TestScanString(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"hello world, this is a clean string without problems yeah ok", 60},
		{"hello\"world", 5},
		{"hello\\world", 5},
		{"hello\nworld", 5},
		{"hello<world", 5},
		{"hello>world", 5},
		{"hello&world", 5},
		{"hello\u2028world", 5},
		{string([]byte{'h', 'i', 0xff}), 2},
		{strings.Repeat("a", 100), 100},
		{strings.Repeat("a", 100) + "\"tail", 100},
		{strings.Repeat("a", 63) + "\"tail", 63},
		{strings.Repeat("a", 64) + "\"tail", 64},
		{strings.Repeat("a", 65) + "\"tail", 65},
		{strings.Repeat("a", 128) + "\"tail", 128},
	}
	for i, c := range cases {
		n := len(c.s)
		p := unsafe.Pointer(unsafe.StringData(c.s))
		if got := scanString(p, n); got != c.want {
			t.Errorf("case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
		// also test SWAR fallback path
		if got := scanStringSWAR(p, n); got != c.want {
			t.Errorf("SWAR case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
	}
}

func BenchmarkScanStringSIMD(b *testing.B) {
	s := strings.Repeat("the quick brown fox ", 50) // 1000 bytes clean
	p := unsafe.Pointer(unsafe.StringData(s))
	n := len(s)
	b.SetBytes(int64(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scanStringSIMD((*byte)(p), n)
	}
}

func BenchmarkScanStringSWAR(b *testing.B) {
	s := strings.Repeat("the quick brown fox ", 50)
	p := unsafe.Pointer(unsafe.StringData(s))
	n := len(s)
	b.SetBytes(int64(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scanStringSWAR(p, n)
	}
}

// --- skipWS kernel ---

func TestSkipWSSIMD(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"   \t\n\rhello", 6},
		{"", 0},
		{"x", 0},
		{"   ", 3},
		{strings.Repeat(" ", 100) + "x", 100},
		{strings.Repeat(" ", 64) + "hello", 64},
		{strings.Repeat(" ", 63) + "hello", 63},
		{strings.Repeat(" ", 65) + "hello", 65},
		{strings.Repeat("\t\n\r ", 32) + "x", 128},
	}
	for i, c := range cases {
		n := len(c.s)
		var p *byte
		if n > 0 {
			p = unsafe.StringData(c.s)
		}
		if got := skipWSSIMD(p, n); got != c.want {
			t.Errorf("case %d (%q): got %d, want %d", i, c.s, got, c.want)
		}
	}
}

func BenchmarkSkipWSSIMD(b *testing.B) {
	s := strings.Repeat("  \t\n  \t\n  \t\n  \t\n", 64) + "end" // 1024 WS + 3 bytes
	p := unsafe.StringData(s)
	n := len(s)
	b.SetBytes(int64(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = skipWSSIMD(p, n)
	}
}

func BenchmarkSkipWSScalar(b *testing.B) {
	s := strings.Repeat("  \t\n  \t\n  \t\n  \t\n", 64) + "end"
	n := len(s)
	b.SetBytes(int64(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := 0
		for p < n {
			c := s[p]
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				break
			}
			p++
		}
		_ = p
	}
}
