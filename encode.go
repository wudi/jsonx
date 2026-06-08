package jsonx

import (
	"math"
	"reflect"
	"strconv"
	"sync"
	"unicode/utf8"
	"unsafe"
)

type encoder struct {
	buf []byte
}

var encoderPool = sync.Pool{New: func() interface{} { return &encoder{buf: make([]byte, 0, 1024)} }}

func (e *encoder) reset() { e.buf = e.buf[:0] }

func (e *encoder) encode(v interface{}) error {
	if v == nil {
		e.buf = append(e.buf, 'n', 'u', 'l', 'l')
		return nil
	}
	// Fast path for common scalar interface values.
	switch x := v.(type) {
	case string:
		e.writeString(x)
		return nil
	case bool:
		if x {
			e.buf = append(e.buf, 't', 'r', 'u', 'e')
		} else {
			e.buf = append(e.buf, 'f', 'a', 'l', 's', 'e')
		}
		return nil
	case float64:
		return e.writeFloat(x, 64)
	case int:
		e.buf = strconv.AppendInt(e.buf, int64(x), 10)
		return nil
	case int64:
		e.buf = strconv.AppendInt(e.buf, x, 10)
		return nil
	case map[string]interface{}:
		return e.writeMapInterface(x)
	case []interface{}:
		return e.writeSliceInterface(x)
	case []byte:
		if x == nil {
			e.buf = append(e.buf, 'n', 'u', 'l', 'l')
			return nil
		}
		e.buf = append(e.buf, '"')
		e.buf = base64Encode(e.buf, x)
		e.buf = append(e.buf, '"')
		return nil
	}
	rv := reflect.ValueOf(v)
	enc := cachedEncoder(rv.Type())
	return enc(e, unsafePointerOf(rv))
}

func unsafePointerOf(rv reflect.Value) unsafe.Pointer {
	// For most kinds the reflect.Value holds the value itself (not a pointer).
	// We need a pointer to that value. Use Addr() if addressable; otherwise
	// copy to a heap slot.
	if rv.CanAddr() {
		return unsafe.Pointer(rv.UnsafeAddr())
	}
	// Make addressable via a new pointer of the same type.
	p := reflect.New(rv.Type())
	p.Elem().Set(rv)
	return unsafe.Pointer(p.Pointer())
}

// -------- string writing --------

var htmlEscape = [256]bool{'<': true, '>': true, '&': true}

func (e *encoder) writeString(s string) {
	n := len(s)
	if n == 0 {
		e.buf = append(e.buf, '"', '"')
		return
	}
	var i int
	if hasFastScan && n >= 64 {
		i = scanStringSIMD(unsafe.StringData(s), n)
	} else {
		// Inline 8-byte SWAR scan.
		sp := unsafe.StringData(s)
		for i+8 <= n {
			w := *(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(sp)) + uintptr(i)))
			if hasQuoteOrBackslashOrCtl(w) {
				break
			}
			i += 8
		}
		for i < n {
			c := s[i]
			if c == '"' || c == '\\' || c < 0x20 {
				break
			}
			i++
		}
	}
	if i == n {
		// Fast path: no escapes. One combined grow check + copy instead
		// of three separate appends (each one does its own grow check +
		// slice-header write, the latter provoking GC write barriers).
		L := len(e.buf)
		need := L + n + 2
		if need <= cap(e.buf) {
			buf := e.buf[:need]
			buf[L] = '"'
			copy(buf[L+1:], s)
			buf[need-1] = '"'
			e.buf = buf
			return
		}
		// slow path via append (grows)
		e.buf = append(e.buf, '"')
		e.buf = append(e.buf, s...)
		e.buf = append(e.buf, '"')
		return
	}
	e.buf = append(e.buf, '"')
	e.writeStringSlow(s, i)
}

func (e *encoder) writeStringSlow(s string, start int) {
	// copy the already-scanned prefix
	e.buf = append(e.buf, s[:start]...)
	for i := start; i < len(s); {
		c := s[i]
		if c < 0x20 {
			switch c {
			case '\n':
				e.buf = append(e.buf, '\\', 'n')
			case '\r':
				e.buf = append(e.buf, '\\', 'r')
			case '\t':
				e.buf = append(e.buf, '\\', 't')
			case '\b':
				e.buf = append(e.buf, '\\', 'b')
			case '\f':
				e.buf = append(e.buf, '\\', 'f')
			default:
				e.buf = append(e.buf, '\\', 'u', '0', '0', hexChar[c>>4], hexChar[c&0xf])
			}
			i++
			continue
		}
		if c == '"' {
			e.buf = append(e.buf, '\\', '"')
			i++
			continue
		}
		if c == '\\' {
			e.buf = append(e.buf, '\\', '\\')
			i++
			continue
		}
		// handle valid UTF-8 fast
		if c < utf8.RuneSelf {
			e.buf = append(e.buf, c)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			e.buf = append(e.buf, '\\', 'u', 'f', 'f', 'f', 'd')
			i++
			continue
		}
		e.buf = append(e.buf, s[i:i+size]...)
		i += size
	}
	e.buf = append(e.buf, '"')
}

var hexChar = "0123456789abcdef"

// appendIndented reformats compact JSON from src into dst, inserting newlines,
// prefix, and per-level indent matching encoding/json.Indent semantics. src is
// assumed to be valid, compact JSON as produced by Marshal.
func appendIndented(dst, src []byte, prefix, indent string) []byte {
	depth := 0
	inString := false
	escape := false
	needIndent := false
	writeIndent := func(dst []byte, d int) []byte {
		dst = append(dst, '\n')
		dst = append(dst, prefix...)
		for i := 0; i < d; i++ {
			dst = append(dst, indent...)
		}
		return dst
	}
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			dst = append(dst, c)
			if escape {
				escape = false
				continue
			}
			switch c {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		if needIndent && c != '}' && c != ']' {
			dst = writeIndent(dst, depth)
		}
		needIndent = false
		switch c {
		case '"':
			inString = true
			dst = append(dst, c)
		case '{', '[':
			depth++
			dst = append(dst, c)
			needIndent = true
		case '}', ']':
			depth--
			if n := len(dst); n > 0 && (dst[n-1] == '{' || dst[n-1] == '[') {
				dst = append(dst, c)
			} else {
				dst = writeIndent(dst, depth)
				dst = append(dst, c)
			}
		case ',':
			dst = append(dst, c)
			needIndent = true
		case ':':
			dst = append(dst, c, ' ')
		case ' ', '\t', '\n', '\r':
			// Marshal doesn't emit whitespace outside strings; ignore defensively.
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

// -------- number writing --------

func (e *encoder) writeFloat(f float64, bits int) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return &UnsupportedTypeError{Type: reflect.TypeOf(f)}
	}
	// float64 → pure-Go Schubfach (E21). ~41 % faster than strconv
	// shortest in isolation microbench (458 ns vs 777 ns on canada
	// samples). Round-trip identical to strconv (fuzzed against 1 M
	// random + all 111 k canada.json floats, all bit-exact).
	if bits == 64 {
		e.buf = schubfachAppendFloat64(e.buf, f)
		return nil
	}
	// float32: keep stdlib for now; doesn't affect our corpora.
	abs := math.Abs(f)
	fmt := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		fmt = 'e'
	}
	e.buf = strconv.AppendFloat(e.buf, f, fmt, -1, 32)
	return nil
}

// -------- map[string]interface{} / []interface{} --------

func (e *encoder) writeMapInterface(m map[string]interface{}) error {
	e.buf = append(e.buf, '{')
	first := true
	for k, v := range m {
		if !first {
			e.buf = append(e.buf, ',')
		}
		first = false
		e.writeString(k)
		e.buf = append(e.buf, ':')
		if err := e.encodeAny(v); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, '}')
	return nil
}

func (e *encoder) writeSliceInterface(a []interface{}) error {
	e.buf = append(e.buf, '[')
	for i, v := range a {
		if i > 0 {
			e.buf = append(e.buf, ',')
		}
		if err := e.encodeAny(v); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, ']')
	return nil
}

// encodeAny dispatches an interface{} value using direct type-pointer
// comparison — faster than Go's type-switch assembly for a small, fixed
// set of "hot" types (strings, float64, map/slice of interface{}). On
// twitter.json this removed ≈ 18 % GC write-barrier overhead that the
// type-switch assembly was triggering via implicit iface copies.
func (e *encoder) encodeAny(v interface{}) error {
	ef := (*eface)(unsafe.Pointer(&v))
	tp := ef.typ
	if tp == nil {
		e.buf = append(e.buf, 'n', 'u', 'l', 'l')
		return nil
	}
	switch tp {
	case typeString:
		// data points to a string header (len,data)
		s := *(*string)(ef.data)
		e.writeString(s)
		return nil
	case typeFloat64:
		f := *(*float64)(ef.data)
		return e.writeFloat(f, 64)
	case typeBool:
		if *(*bool)(ef.data) {
			e.buf = append(e.buf, 't', 'r', 'u', 'e')
		} else {
			e.buf = append(e.buf, 'f', 'a', 'l', 's', 'e')
		}
		return nil
	case typeMapStringInterface:
		// A Go map is internally a pointer (hmap*), so the iface data
		// field already IS the map pointer — reinterpret &ef.data, not
		// the pointee.
		return e.writeMapInterface(*(*map[string]interface{})(unsafe.Pointer(&ef.data)))
	case typeSliceInterface:
		// A slice header is 24 bytes, so Go boxes it — ef.data points
		// at the header.
		return e.writeSliceInterface(*(*[]interface{})(ef.data))
	case typeInt:
		e.buf = strconv.AppendInt(e.buf, int64(*(*int)(ef.data)), 10)
		return nil
	case typeInt64:
		e.buf = strconv.AppendInt(e.buf, *(*int64)(ef.data), 10)
		return nil
	}
	// Fallback for less-common types: reflect path.
	return e.encode(v)
}
