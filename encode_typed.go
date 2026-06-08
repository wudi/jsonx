package jsonx

import (
	"reflect"
	"strconv"
	"sync"
	"unsafe"
)

type typedEncodeFn func(e *encoder, p unsafe.Pointer) error

var encTypeCache sync.Map

func cachedEncoder(t reflect.Type) typedEncodeFn {
	if fn, ok := encTypeCache.Load(t); ok {
		return fn.(typedEncodeFn)
	}
	fn := buildEncoder(t)
	encTypeCache.Store(t, fn)
	return fn
}

func buildEncoder(t reflect.Type) typedEncodeFn {
	// json.Marshaler / encoding.TextMarshaler take priority over the kind
	// switch. Pointer kinds defer to buildPtrEncoder so nil → "null" is
	// handled before we attempt to call MarshalJSON on a nil receiver.
	if t.Kind() != reflect.Ptr {
		if t.Implements(marshalerType) {
			return buildMarshalerEncoder(t, false)
		}
		if reflect.PtrTo(t).Implements(marshalerType) {
			return buildMarshalerEncoder(t, true)
		}
		if t.Implements(textMarshalerType) {
			return buildTextMarshalerEncoder(t, false)
		}
		if reflect.PtrTo(t).Implements(textMarshalerType) {
			return buildTextMarshalerEncoder(t, true)
		}
	}
	switch t.Kind() {
	case reflect.String:
		return encString
	case reflect.Bool:
		return encBool
	case reflect.Int:
		return encInt
	case reflect.Int8:
		return encInt8
	case reflect.Int16:
		return encInt16
	case reflect.Int32:
		return encInt32
	case reflect.Int64:
		return encInt64
	case reflect.Uint:
		return encUint
	case reflect.Uint8:
		return encUint8
	case reflect.Uint16:
		return encUint16
	case reflect.Uint32:
		return encUint32
	case reflect.Uint64:
		return encUint64
	case reflect.Float32:
		return encFloat32
	case reflect.Float64:
		return encFloat64
	case reflect.Ptr:
		return buildPtrEncoder(t)
	case reflect.Slice:
		return buildSliceEncoder(t)
	case reflect.Array:
		return buildArrayEncoder(t)
	case reflect.Map:
		return buildMapEncoder(t)
	case reflect.Struct:
		return buildStructEncoder(t)
	case reflect.Interface:
		return encInterface
	}
	tt := t
	return func(e *encoder, p unsafe.Pointer) error { return &UnsupportedTypeError{Type: tt} }
}

func encString(e *encoder, p unsafe.Pointer) error {
	e.writeString(*(*string)(p))
	return nil
}
func encBool(e *encoder, p unsafe.Pointer) error {
	if *(*bool)(p) {
		e.buf = append(e.buf, 't', 'r', 'u', 'e')
	} else {
		e.buf = append(e.buf, 'f', 'a', 'l', 's', 'e')
	}
	return nil
}
func encInt(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendInt(e.buf, int64(*(*int)(p)), 10)
	return nil
}
func encInt8(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendInt(e.buf, int64(*(*int8)(p)), 10)
	return nil
}
func encInt16(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendInt(e.buf, int64(*(*int16)(p)), 10)
	return nil
}
func encInt32(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendInt(e.buf, int64(*(*int32)(p)), 10)
	return nil
}
func encInt64(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendInt(e.buf, *(*int64)(p), 10)
	return nil
}
func encUint(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendUint(e.buf, uint64(*(*uint)(p)), 10)
	return nil
}
func encUint8(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendUint(e.buf, uint64(*(*uint8)(p)), 10)
	return nil
}
func encUint16(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendUint(e.buf, uint64(*(*uint16)(p)), 10)
	return nil
}
func encUint32(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendUint(e.buf, uint64(*(*uint32)(p)), 10)
	return nil
}
func encUint64(e *encoder, p unsafe.Pointer) error {
	e.buf = strconv.AppendUint(e.buf, *(*uint64)(p), 10)
	return nil
}
func encFloat32(e *encoder, p unsafe.Pointer) error {
	return e.writeFloat(float64(*(*float32)(p)), 32)
}
func encFloat64(e *encoder, p unsafe.Pointer) error {
	return e.writeFloat(*(*float64)(p), 64)
}

func buildPtrEncoder(t reflect.Type) typedEncodeFn {
	elem := t.Elem()
	elemEnc := cachedEncoder(elem)
	return func(e *encoder, p unsafe.Pointer) error {
		pp := *(*unsafe.Pointer)(p)
		if pp == nil {
			e.buf = append(e.buf, 'n', 'u', 'l', 'l')
			return nil
		}
		return elemEnc(e, pp)
	}
}

func buildSliceEncoder(t reflect.Type) typedEncodeFn {
	elem := t.Elem()
	if elem.Kind() == reflect.Uint8 {
		return encByteSlice
	}
	elemEnc := cachedEncoder(elem)
	elemSize := elem.Size()
	return func(e *encoder, p unsafe.Pointer) error {
		sh := (*sliceHeader)(p)
		if sh.Data == nil {
			e.buf = append(e.buf, 'n', 'u', 'l', 'l')
			return nil
		}
		e.buf = append(e.buf, '[')
		for i := 0; i < sh.Len; i++ {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			if err := elemEnc(e, unsafe.Pointer(uintptr(sh.Data)+uintptr(i)*elemSize)); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, ']')
		return nil
	}
}

func buildArrayEncoder(t reflect.Type) typedEncodeFn {
	elem := t.Elem()
	elemEnc := cachedEncoder(elem)
	elemSize := elem.Size()
	n := t.Len()
	return func(e *encoder, p unsafe.Pointer) error {
		e.buf = append(e.buf, '[')
		for i := 0; i < n; i++ {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			if err := elemEnc(e, unsafe.Pointer(uintptr(p)+uintptr(i)*elemSize)); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, ']')
		return nil
	}
}

func encByteSlice(e *encoder, p unsafe.Pointer) error {
	s := *(*[]byte)(p)
	if s == nil {
		e.buf = append(e.buf, 'n', 'u', 'l', 'l')
		return nil
	}
	e.buf = append(e.buf, '"')
	e.buf = base64Encode(e.buf, s)
	e.buf = append(e.buf, '"')
	return nil
}

func buildMapEncoder(t reflect.Type) typedEncodeFn {
	if t.Key().Kind() != reflect.String {
		tt := t
		return func(e *encoder, p unsafe.Pointer) error { return &UnsupportedTypeError{Type: tt} }
	}
	tt := t
	return func(e *encoder, p unsafe.Pointer) error {
		mv := reflect.NewAt(tt, p).Elem()
		if mv.IsNil() {
			e.buf = append(e.buf, 'n', 'u', 'l', 'l')
			return nil
		}
		e.buf = append(e.buf, '{')
		first := true
		iter := mv.MapRange()
		for iter.Next() {
			if !first {
				e.buf = append(e.buf, ',')
			}
			first = false
			e.writeString(iter.Key().String())
			e.buf = append(e.buf, ':')
			v := iter.Value().Interface()
			if err := e.encode(v); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, '}')
		return nil
	}
}

type encField struct {
	name   []byte // pre-quoted `,"name":` or `"name":` for first
	first  []byte // `"name":` (no leading comma)
	offset uintptr
	enc    typedEncodeFn
	omit   bool // omitempty
	typ    reflect.Type
}

func buildStructEncoder(t reflect.Type) typedEncodeFn {
	var fields []encField
	n := t.NumField()
	for i := 0; i < n; i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		omit := false
		if tag != "" {
			parts := splitTag(tag)
			if parts[0] != "" {
				name = parts[0]
			}
			for _, o := range parts[1:] {
				if o == "omitempty" {
					omit = true
				}
			}
		}
		// pre-build quoted key
		firstKey := appendJSONString(nil, name)
		firstKey = append(firstKey, ':')
		commaKey := append([]byte{','}, firstKey...)
		fields = append(fields, encField{
			name:   commaKey,
			first:  firstKey,
			offset: f.Offset,
			enc:    cachedEncoder(f.Type),
			omit:   omit,
			typ:    f.Type,
		})
	}
	return func(e *encoder, p unsafe.Pointer) error {
		e.buf = append(e.buf, '{')
		started := false
		for i := range fields {
			f := &fields[i]
			fp := unsafe.Add(p, f.offset)
			if f.omit && isZero(fp, f.typ) {
				continue
			}
			if started {
				e.buf = append(e.buf, f.name...)
			} else {
				e.buf = append(e.buf, f.first...)
				started = true
			}
			if err := f.enc(e, fp); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, '}')
		return nil
	}
}

func splitTag(tag string) []string {
	parts := []string{}
	s := 0
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			parts = append(parts, tag[s:i])
			s = i + 1
		}
	}
	parts = append(parts, tag[s:])
	return parts
}

func isZero(p unsafe.Pointer, t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String:
		return *(*string)(p) == ""
	case reflect.Bool:
		return !*(*bool)(p)
	case reflect.Int, reflect.Int64:
		return *(*int64)(p) == 0
	case reflect.Int32:
		return *(*int32)(p) == 0
	case reflect.Int16:
		return *(*int16)(p) == 0
	case reflect.Int8:
		return *(*int8)(p) == 0
	case reflect.Uint, reflect.Uint64:
		return *(*uint64)(p) == 0
	case reflect.Uint32:
		return *(*uint32)(p) == 0
	case reflect.Uint16:
		return *(*uint16)(p) == 0
	case reflect.Uint8:
		return *(*uint8)(p) == 0
	case reflect.Float32:
		return *(*float32)(p) == 0
	case reflect.Float64:
		return *(*float64)(p) == 0
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface:
		return *(*unsafe.Pointer)(p) == nil
	}
	return false
}

func encInterface(e *encoder, p unsafe.Pointer) error {
	iv := *(*interface{})(p)
	return e.encode(iv)
}
