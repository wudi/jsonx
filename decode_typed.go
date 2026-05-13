package jsonx

import (
	"reflect"
	"strconv"
	"sync"
	"unsafe"

	_ "unsafe" // for go:linkname
)

// reflect_unsafe_NewArray is the runtime entry point reflect.MakeSlice uses
// internally — we call it directly to skip the reflect.SliceOf + Value
// wrapping that MakeSlice does on the hot slice-growth path.
//
//go:linkname reflect_unsafe_NewArray reflect.unsafe_NewArray
func reflect_unsafe_NewArray(typ unsafe.Pointer, n int) unsafe.Pointer

// rtypeOf returns the internal *rtype pointer that backs a reflect.Type.
// reflect.Type is an interface whose data word IS the *rtype.
func rtypeOf(t reflect.Type) unsafe.Pointer {
	type iface struct {
		itab unsafe.Pointer
		data unsafe.Pointer
	}
	return (*iface)(unsafe.Pointer(&t)).data
}

// typedDecodeFn decodes JSON at d.p into *p (pointer to a Go value of a known
// static type). The plan is built at first sighting and cached per type.
type typedDecodeFn func(d *decoder, p unsafe.Pointer) error

var typeCache sync.Map // map[reflect.Type]typedDecodeFn

func cachedDecoder(t reflect.Type) typedDecodeFn {
	if fn, ok := typeCache.Load(t); ok {
		return fn.(typedDecodeFn)
	}
	fn := buildDecoder(t)
	typeCache.Store(t, fn)
	return fn
}

func buildDecoder(t reflect.Type) typedDecodeFn {
	// json.Unmarshaler / encoding.TextUnmarshaler win over the kind switch —
	// including for slices (json.RawMessage is []byte but must NOT go through
	// the base64 path). Pointer kinds defer: buildPtrDecoder handles null →
	// nil first, then recurses into the element where this check runs again.
	if t.Kind() != reflect.Ptr {
		if reflect.PtrTo(t).Implements(unmarshalerType) {
			return buildUnmarshalerDecoder(t)
		}
		if reflect.PtrTo(t).Implements(textUnmarshalerType) {
			return buildTextUnmarshalerDecoder(t)
		}
	}
	switch t.Kind() {
	case reflect.String:
		return decString
	case reflect.Bool:
		return decBool
	case reflect.Int:
		return decInt
	case reflect.Int8:
		return decInt8
	case reflect.Int16:
		return decInt16
	case reflect.Int32:
		return decInt32
	case reflect.Int64:
		return decInt64
	case reflect.Uint:
		return decUint
	case reflect.Uint8:
		return decUint8
	case reflect.Uint16:
		return decUint16
	case reflect.Uint32:
		return decUint32
	case reflect.Uint64:
		return decUint64
	case reflect.Float32:
		return decFloat32
	case reflect.Float64:
		return decFloat64
	case reflect.Ptr:
		return buildPtrDecoder(t)
	case reflect.Slice:
		return buildSliceDecoder(t)
	case reflect.Array:
		return buildArrayDecoder(t)
	case reflect.Map:
		return buildMapDecoder(t)
	case reflect.Struct:
		return buildStructDecoder(t)
	case reflect.Interface:
		return decInterface
	}
	tt := t
	return func(d *decoder, p unsafe.Pointer) error {
		return &UnsupportedTypeError{Type: tt}
	}
}

// -------- scalar decoders --------

// classifyMismatch maps a leading JSON byte to the value-name used in
// *UnmarshalTypeError. Returns "" if the byte does not begin a JSON value
// (i.e. true syntax error, not a type mismatch). Note: 'n' (null) is
// intentionally omitted — the scalar decoders that accept null-as-zero
// intercept it earlier, and decoders that don't currently support null
// keep their pre-existing syntax-error semantics.
func classifyMismatch(c byte) string {
	switch {
	case c == '[':
		return "array"
	case c == '{':
		return "object"
	case c == '"':
		return "string"
	case c == 't' || c == 'f':
		return "bool"
	case c == '-' || (c >= '0' && c <= '9'):
		return "number"
	}
	return ""
}

// Cached reflect.Type values for scalar decoders. Filled into
// *UnmarshalTypeError so callers see the concrete Go type that mismatched.
var (
	stringType  = reflect.TypeOf("")
	boolType    = reflect.TypeOf(false)
	intType     = reflect.TypeOf(int(0))
	int8Type    = reflect.TypeOf(int8(0))
	int16Type   = reflect.TypeOf(int16(0))
	int32Type   = reflect.TypeOf(int32(0))
	int64Type   = reflect.TypeOf(int64(0))
	uintType    = reflect.TypeOf(uint(0))
	uint8Type   = reflect.TypeOf(uint8(0))
	uint16Type  = reflect.TypeOf(uint16(0))
	uint32Type  = reflect.TypeOf(uint32(0))
	uint64Type  = reflect.TypeOf(uint64(0))
	float32Type = reflect.TypeOf(float32(0))
	float64Type = reflect.TypeOf(float64(0))
)

// fillType sets Type on a *UnmarshalTypeError when not already populated.
// Used by typed wrappers (decInt8, etc.) over shared parsers (readInt) so
// the error reports the concrete target type.
func fillType(err error, t reflect.Type) error {
	if ute, ok := err.(*UnmarshalTypeError); ok && ute.Type == nil {
		ute.Type = t
	}
	return err
}

func decString(d *decoder, p unsafe.Pointer) error {
	d.skipWS()
	if d.p >= len(d.data) {
		return syntaxErr("expected string", d.p)
	}
	c := d.data[d.p]
	if c == 'n' {
		if err := d.decodeNull(); err != nil {
			return err
		}
		*(*string)(p) = ""
		return nil
	}
	if c != '"' {
		if v := classifyMismatch(c); v != "" {
			return &UnmarshalTypeError{Value: v, Type: stringType, Offset: int64(d.p)}
		}
		return syntaxErr("expected string", d.p)
	}
	s, err := d.decodeString()
	if err != nil {
		return err
	}
	// For struct fields we must copy — the lifetime of input > the output
	// target only while the caller holds data. Keep the copy.
	*(*string)(p) = string(s)
	return nil
}

func decBool(d *decoder, p unsafe.Pointer) error {
	d.skipWS()
	if d.p >= len(d.data) {
		return syntaxErr("expected bool", d.p)
	}
	c := d.data[d.p]
	if c != 't' && c != 'f' {
		if v := classifyMismatch(c); v != "" {
			return &UnmarshalTypeError{Value: v, Type: boolType, Offset: int64(d.p)}
		}
		// fall through to decodeBool for the canonical syntax error
	}
	b, err := d.decodeBool()
	if err != nil {
		return err
	}
	*(*bool)(p) = b
	return nil
}

func decInt(d *decoder, p unsafe.Pointer) error {
	n, err := d.readInt()
	if err != nil {
		return fillType(err, intType)
	}
	*(*int)(p) = int(n)
	return nil
}
func decInt8(d *decoder, p unsafe.Pointer) error {
	n, err := d.readInt()
	if err != nil {
		return fillType(err, int8Type)
	}
	*(*int8)(p) = int8(n)
	return nil
}
func decInt16(d *decoder, p unsafe.Pointer) error {
	n, err := d.readInt()
	if err != nil {
		return fillType(err, int16Type)
	}
	*(*int16)(p) = int16(n)
	return nil
}
func decInt32(d *decoder, p unsafe.Pointer) error {
	n, err := d.readInt()
	if err != nil {
		return fillType(err, int32Type)
	}
	*(*int32)(p) = int32(n)
	return nil
}
func decInt64(d *decoder, p unsafe.Pointer) error {
	n, err := d.readInt()
	if err != nil {
		return fillType(err, int64Type)
	}
	*(*int64)(p) = n
	return nil
}
func decUint(d *decoder, p unsafe.Pointer) error {
	n, err := d.readUint()
	if err != nil {
		return fillType(err, uintType)
	}
	*(*uint)(p) = uint(n)
	return nil
}
func decUint8(d *decoder, p unsafe.Pointer) error {
	n, err := d.readUint()
	if err != nil {
		return fillType(err, uint8Type)
	}
	*(*uint8)(p) = uint8(n)
	return nil
}
func decUint16(d *decoder, p unsafe.Pointer) error {
	n, err := d.readUint()
	if err != nil {
		return fillType(err, uint16Type)
	}
	*(*uint16)(p) = uint16(n)
	return nil
}
func decUint32(d *decoder, p unsafe.Pointer) error {
	n, err := d.readUint()
	if err != nil {
		return fillType(err, uint32Type)
	}
	*(*uint32)(p) = uint32(n)
	return nil
}
func decUint64(d *decoder, p unsafe.Pointer) error {
	n, err := d.readUint()
	if err != nil {
		return fillType(err, uint64Type)
	}
	*(*uint64)(p) = n
	return nil
}
func decFloat32(d *decoder, p unsafe.Pointer) error {
	d.skipWS()
	if d.p < len(d.data) {
		c := d.data[d.p]
		if c != '-' && (c < '0' || c > '9') && c != 'n' {
			if v := classifyMismatch(c); v != "" {
				return &UnmarshalTypeError{Value: v, Type: float32Type, Offset: int64(d.p)}
			}
		}
	}
	raw, err := d.decodeNumberSlice()
	if err != nil {
		return err
	}
	v, err := strconv.ParseFloat(b2sUnsafe(raw), 32)
	if err != nil {
		return syntaxErr("invalid float", d.p)
	}
	*(*float32)(p) = float32(v)
	return nil
}
func decFloat64(d *decoder, p unsafe.Pointer) error {
	d.skipWS()
	if d.p < len(d.data) {
		c := d.data[d.p]
		if c != '-' && (c < '0' || c > '9') && c != 'n' {
			if v := classifyMismatch(c); v != "" {
				return &UnmarshalTypeError{Value: v, Type: float64Type, Offset: int64(d.p)}
			}
		}
	}
	v, err := d.scanNumber()
	if err != nil {
		return err
	}
	*(*float64)(p) = v
	return nil
}

// readInt/readUint: signed/unsigned fast integer parse, falling back to float
// if the literal has a fraction/exponent (matching encoding/json).
func (d *decoder) readInt() (int64, error) {
	d.skipWS()
	b := d.data
	p := d.p
	if p >= len(b) {
		return 0, syntaxErr("expected number", p)
	}
	if b[p] == 'n' { // null -> zero
		if err := d.decodeNull(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	// Type-mismatch fast path: anything that's a valid JSON value start but
	// not a number gets an *UnmarshalTypeError (Type filled by the caller).
	if c := b[p]; c != '-' && (c < '0' || c > '9') {
		if v := classifyMismatch(c); v != "" {
			return 0, &UnmarshalTypeError{Value: v, Offset: int64(p)}
		}
		return 0, syntaxErr("invalid int", p)
	}
	neg := false
	if b[p] == '-' {
		neg = true
		p++
	}
	if p >= len(b) || b[p] < '0' || b[p] > '9' {
		return 0, syntaxErr("invalid int", p)
	}
	var v uint64
	for p < len(b) && b[p] >= '0' && b[p] <= '9' {
		v = v*10 + uint64(b[p]-'0')
		p++
	}
	// if there's a fraction/exponent, re-parse via strconv (losing precision accepted)
	if p < len(b) && (b[p] == '.' || b[p] == 'e' || b[p] == 'E') {
		start := d.p
		d.p = p
		for p < len(b) {
			c := b[p]
			if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
				p++
				continue
			}
			break
		}
		d.p = p
		f, err := strconv.ParseFloat(b2sUnsafe(b[start:p]), 64)
		if err != nil {
			return 0, syntaxErr("invalid int", start)
		}
		return int64(f), nil
	}
	d.p = p
	if neg {
		return -int64(v), nil
	}
	return int64(v), nil
}

func (d *decoder) readUint() (uint64, error) {
	n, err := d.readInt()
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, nil
	}
	return uint64(n), nil
}

// -------- pointer --------

func buildPtrDecoder(t reflect.Type) typedDecodeFn {
	elem := t.Elem()
	elemDec := cachedDecoder(elem)
	elemType := elem
	return func(d *decoder, p unsafe.Pointer) error {
		d.skipWS()
		if d.p < len(d.data) && d.data[d.p] == 'n' {
			if err := d.decodeNull(); err != nil {
				return err
			}
			*(*unsafe.Pointer)(p) = nil
			return nil
		}
		pp := *(*unsafe.Pointer)(p)
		if pp == nil {
			// allocate via reflect to satisfy GC bookkeeping
			v := reflect.New(elemType)
			*(*unsafe.Pointer)(p) = unsafe.Pointer(v.Pointer())
			pp = *(*unsafe.Pointer)(p)
		}
		return elemDec(d, pp)
	}
}

// -------- slice / array / map --------

func buildSliceDecoder(t reflect.Type) typedDecodeFn {
	elem := t.Elem()
	// []byte fast path: base64 per encoding/json spec.
	if elem.Kind() == reflect.Uint8 {
		return decByteSlice
	}
	elemDec := cachedDecoder(elem)
	elemSize := elem.Size()
	elemRtype := rtypeOf(elem) // cache the *rtype for direct mallocgc path.
	tt := t
	return func(d *decoder, p unsafe.Pointer) error {
		d.skipWS()
		if d.p >= len(d.data) {
			return syntaxErr("expected array", d.p)
		}
		if d.data[d.p] == 'n' {
			if err := d.decodeNull(); err != nil {
				return err
			}
			// zero the slice header
			*(*[3]uintptr)(p) = [3]uintptr{}
			return nil
		}
		if d.data[d.p] != '[' {
			v := classifyMismatch(d.data[d.p])
			if v == "" {
				v = "non-array"
			}
			return &UnmarshalTypeError{Value: v, Type: tt, Offset: int64(d.p)}
		}
		d.p++
		// Element decoders do their own leading skipWS; merging that with
		// the empty-array `]` check lets us avoid the redundant skipWS that
		// used to sit here. The first element decoder's skipWS also handles
		// the whitespace between `[` and the first element.
		sh := (*sliceHeader)(p)
		sh.Len = 0
		for {
			// Check for empty array / trailing `]` before decoding element.
			// We have to skipWS here anyway because the element decoder
			// would otherwise error on `]`.
			d.skipWS()
			if d.p >= len(d.data) {
				return syntaxErr("unexpected end in array", d.p)
			}
			if d.data[d.p] == ']' {
				d.p++
				return nil
			}
			// grow if needed
			if sh.Len >= sh.Cap {
				growSlice(sh, elemRtype, elemSize)
			}
			elemPtr := unsafe.Pointer(uintptr(sh.Data) + uintptr(sh.Len)*elemSize)
			if err := elemDec(d, elemPtr); err != nil {
				return err
			}
			sh.Len++
			// Fast-path: no whitespace between element and `,` or `]`.
			if d.p < len(d.data) {
				c := d.data[d.p]
				if c == ',' {
					d.p++
					continue
				}
				if c == ']' {
					d.p++
					return nil
				}
			}
			d.skipWS()
			if d.p >= len(d.data) {
				return syntaxErr("unexpected end in array", d.p)
			}
			if d.data[d.p] == ',' {
				d.p++
				continue
			}
			if d.data[d.p] == ']' {
				d.p++
				return nil
			}
			return syntaxErr("expected ',' or ']'", d.p)
		}
	}
}

type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

// growSlice allocates a new backing array via runtime.mallocgc (reached through
// reflect.unsafe_NewArray). Skipping reflect.MakeSlice avoids building a
// reflect.Value, calling reflect.SliceOf, and the extra indirection the
// wrapper path costs — worth ~5 % CPU on deep-struct decoding where each
// level grows at least one slice.
func growSlice(sh *sliceHeader, elemRtype unsafe.Pointer, elemSize uintptr) {
	newCap := sh.Cap * 2
	if newCap < 4 {
		newCap = 4
	}
	newData := reflect_unsafe_NewArray(elemRtype, newCap)
	if sh.Len > 0 {
		src := unsafe.Slice((*byte)(sh.Data), sh.Len*int(elemSize))
		dst := unsafe.Slice((*byte)(newData), sh.Len*int(elemSize))
		copy(dst, src)
	}
	sh.Data = newData
	sh.Cap = newCap
}

func buildArrayDecoder(t reflect.Type) typedDecodeFn {
	elem := t.Elem()
	elemDec := cachedDecoder(elem)
	elemSize := elem.Size()
	n := t.Len()
	tt := t
	return func(d *decoder, p unsafe.Pointer) error {
		d.skipWS()
		if d.p >= len(d.data) || d.data[d.p] != '[' {
			v := ""
			if d.p < len(d.data) {
				v = classifyMismatch(d.data[d.p])
			}
			if v == "" {
				v = "non-array"
			}
			return &UnmarshalTypeError{Value: v, Type: tt, Offset: int64(d.p)}
		}
		d.p++
		d.skipWS()
		for i := 0; i < n; i++ {
			if d.p < len(d.data) && d.data[d.p] == ']' {
				d.p++
				return nil
			}
			elemPtr := unsafe.Pointer(uintptr(p) + uintptr(i)*elemSize)
			if err := elemDec(d, elemPtr); err != nil {
				return err
			}
			d.skipWS()
			if d.p < len(d.data) && d.data[d.p] == ',' {
				d.p++
			}
		}
		// skip extras
		for d.p < len(d.data) && d.data[d.p] != ']' {
			if _, err := d.decodeAny(); err != nil {
				return err
			}
			d.skipWS()
			if d.p < len(d.data) && d.data[d.p] == ',' {
				d.p++
			}
		}
		if d.p < len(d.data) {
			d.p++
		}
		return nil
	}
}

func decByteSlice(d *decoder, p unsafe.Pointer) error {
	d.skipWS()
	if d.p < len(d.data) && d.data[d.p] == 'n' {
		if err := d.decodeNull(); err != nil {
			return err
		}
		*(*[]byte)(p) = nil
		return nil
	}
	s, err := d.decodeString()
	if err != nil {
		return err
	}
	// base64 decoding
	out, err := base64Decode(s)
	if err != nil {
		return err
	}
	*(*[]byte)(p) = out
	return nil
}

func buildMapDecoder(t reflect.Type) typedDecodeFn {
	if t.Key().Kind() != reflect.String {
		tt := t
		return func(d *decoder, p unsafe.Pointer) error { return &UnsupportedTypeError{Type: tt} }
	}
	elem := t.Elem()
	mapType := t
	// For map[string]interface{} fast path, reuse generic.
	if elem.Kind() == reflect.Interface && elem.NumMethod() == 0 {
		return func(d *decoder, p unsafe.Pointer) error {
			d.skipWS()
			if d.p >= len(d.data) {
				return syntaxErr("expected object", d.p)
			}
			if d.data[d.p] == 'n' {
				if err := d.decodeNull(); err != nil {
					return err
				}
				*(*map[string]interface{})(p) = nil
				return nil
			}
			m, err := d.decodeObject()
			if err != nil {
				return err
			}
			*(*map[string]interface{})(p) = m
			return nil
		}
	}
	elemDec := cachedDecoder(elem)
	elemSize := elem.Size()
	_ = elemSize
	return func(d *decoder, p unsafe.Pointer) error {
		d.skipWS()
		if d.p >= len(d.data) || d.data[d.p] != '{' {
			return syntaxErr("expected object", d.p)
		}
		d.p++
		mv := reflect.NewAt(mapType, p).Elem()
		if mv.IsNil() {
			mv.Set(reflect.MakeMap(mapType))
		}
		elemPtr := reflect.New(elem)
		elemUnsafe := unsafe.Pointer(elemPtr.Pointer())
		d.skipWS()
		if d.p < len(d.data) && d.data[d.p] == '}' {
			d.p++
			return nil
		}
		for {
			d.skipWS()
			key, err := d.decodeString()
			if err != nil {
				return err
			}
			d.skipWS()
			if d.p >= len(d.data) || d.data[d.p] != ':' {
				return syntaxErr("expected ':'", d.p)
			}
			d.p++
			// zero the elem
			typedmemclr(elemUnsafe, elemSize)
			if err := elemDec(d, elemUnsafe); err != nil {
				return err
			}
			mv.SetMapIndex(reflect.ValueOf(key), elemPtr.Elem())
			d.skipWS()
			if d.p >= len(d.data) {
				return syntaxErr("unexpected end", d.p)
			}
			if d.data[d.p] == ',' {
				d.p++
				continue
			}
			if d.data[d.p] == '}' {
				d.p++
				return nil
			}
			return syntaxErr("expected ',' or '}'", d.p)
		}
	}
}

func typedmemclr(p unsafe.Pointer, n uintptr) {
	b := unsafe.Slice((*byte)(p), n)
	for i := range b {
		b[i] = 0
	}
}

// -------- interface{} --------

func decInterface(d *decoder, p unsafe.Pointer) error {
	v, err := d.decodeAny()
	if err != nil {
		return err
	}
	*(*interface{})(p) = v
	return nil
}
