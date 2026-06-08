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
var stringSafeSet = func() [256]bool {
	var safe [256]bool
	for i := 0x20; i < utf8.RuneSelf; i++ {
		c := byte(i)
		safe[c] = c != '"' && c != '\\' && !htmlEscape[c]
	}
	return safe
}()

func (e *encoder) writeString(s string) {
	e.buf = appendJSONString(e.buf, s)
}

func appendJSONString(dst []byte, s string) []byte {
	n := len(s)
	if n == 0 {
		return append(dst, '"', '"')
	}
	i := scanString(unsafe.Pointer(unsafe.StringData(s)), n)
	if i == n {
		// Fast path: no escapes. One combined grow check + copy instead
		// of three separate appends (each one does its own grow check +
		// slice-header write, the latter provoking GC write barriers).
		L := len(dst)
		need := L + n + 2
		if need <= cap(dst) {
			buf := dst[:need]
			buf[L] = '"'
			copy(buf[L+1:], s)
			buf[need-1] = '"'
			return buf
		}
		// slow path via append (grows)
		dst = append(dst, '"')
		dst = append(dst, s...)
		dst = append(dst, '"')
		return dst
	}
	dst = append(dst, '"')
	return appendJSONStringSlow(dst, s, i)
}

func stringEncodeBreakMask(w uint64) uint64 {
	const lo = 0x0101010101010101
	const hi = 0x8080808080808080
	return stringBreakMask(w) |
		byteEqMask(w, '<') |
		byteEqMask(w, '>') |
		byteEqMask(w, '&') |
		(w & hi)
}

func byteEqMask(w uint64, c byte) uint64 {
	const lo = 0x0101010101010101
	const hi = 0x8080808080808080
	x := w ^ (lo * uint64(c))
	return (x - lo) & ^x & hi
}

func appendJSONStringSlow(dst []byte, s string, start int) []byte {
	i := start
	chunkStart := 0
	for i < len(s) {
		c := s[i]
		if c < utf8.RuneSelf {
			if stringSafeSet[c] {
				i++
				continue
			}
			dst = append(dst, s[chunkStart:i]...)
			switch {
			case c == '"' || c == '\\':
				dst = append(dst, '\\', c)
			case c == '\n':
				dst = append(dst, '\\', 'n')
			case c == '\r':
				dst = append(dst, '\\', 'r')
			case c == '\t':
				dst = append(dst, '\\', 't')
			case c == '\b':
				dst = append(dst, '\\', 'b')
			case c == '\f':
				dst = append(dst, '\\', 'f')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexChar[c>>4], hexChar[c&0xf])
			}
			i++
			chunkStart = i
			continue
		}
		switch utf8SeqLen(s, i) {
		case 2:
			i += 2
			continue
		case 3:
			if s[i] == 0xe2 && s[i+1] == 0x80 && s[i+2]&^1 == 0xa8 {
				dst = append(dst, s[chunkStart:i]...)
				dst = append(dst, '\\', 'u', '2', '0', '2', hexChar[s[i+2]&0xf])
				i += 3
				chunkStart = i
				continue
			}
			i += 3
			continue
		case 4:
			i += 4
			continue
		}
		dst = append(dst, s[chunkStart:i]...)
		dst = append(dst, '\\', 'u', 'f', 'f', 'f', 'd')
		i++
		chunkStart = i
	}
	dst = append(dst, s[chunkStart:]...)
	return append(dst, '"')
}

func utf8SeqLen(s string, i int) int {
	c := s[i]
	if c < 0xc2 {
		return 0
	}
	if c < 0xe0 {
		if i+1 < len(s) && isUTF8Cont(s[i+1]) {
			return 2
		}
		return 0
	}
	if c < 0xf0 {
		if i+2 >= len(s) {
			return 0
		}
		c1, c2 := s[i+1], s[i+2]
		if !isUTF8Cont(c1) || !isUTF8Cont(c2) {
			return 0
		}
		if c == 0xe0 && c1 < 0xa0 {
			return 0
		}
		if c == 0xed && c1 >= 0xa0 {
			return 0
		}
		return 3
	}
	if c < 0xf5 {
		if i+3 >= len(s) {
			return 0
		}
		c1, c2, c3 := s[i+1], s[i+2], s[i+3]
		if !isUTF8Cont(c1) || !isUTF8Cont(c2) || !isUTF8Cont(c3) {
			return 0
		}
		if c == 0xf0 && c1 < 0x90 {
			return 0
		}
		if c == 0xf4 && c1 >= 0x90 {
			return 0
		}
		return 4
	}
	return 0
}

func isUTF8Cont(c byte) bool {
	return c&0xc0 == 0x80
}

var hexChar = "0123456789abcdef"

func appendCompactEscapedJSON(dst, src []byte) []byte {
	inString := false
	escape := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			if escape {
				dst = append(dst, c)
				escape = false
				continue
			}
			switch c {
			case '\\':
				dst = append(dst, c)
				escape = true
			case '"':
				dst = append(dst, c)
				inString = false
			case '<', '>', '&':
				dst = append(dst, '\\', 'u', '0', '0', hexChar[c>>4], hexChar[c&0xf])
			case 0xe2:
				if i+2 < len(src) && src[i+1] == 0x80 && src[i+2]&^1 == 0xa8 {
					dst = append(dst, '\\', 'u', '2', '0', '2', hexChar[src[i+2]&0xf])
					i += 2
				} else {
					dst = append(dst, c)
				}
			default:
				dst = append(dst, c)
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			dst = append(dst, c)
		case ' ', '\t', '\n', '\r':
			// compact insignificant whitespace
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

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
