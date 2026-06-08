package jsonx

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"unsafe"
)

// Interface type descriptors captured once so plan-build lookups are pure
// reflect.Type comparisons. We speak encoding/json's interfaces verbatim so
// any type that already satisfies them (including json.RawMessage) works
// without changes.
var (
	unmarshalerType     = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	marshalerType       = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// MarshalerError wraps errors returned from MarshalJSON / MarshalText so the
// origin type is preserved, mirroring encoding/json.MarshalerError.
type MarshalerError struct {
	Type       reflect.Type
	Err        error
	sourceFunc string
}

func (e *MarshalerError) Error() string {
	src := e.sourceFunc
	if src == "" {
		src = "MarshalJSON"
	}
	return fmt.Sprintf("jsonx: error calling %s for type %s: %v", src, e.Type, e.Err)
}

func (e *MarshalerError) Unwrap() error { return e.Err }

// buildUnmarshalerDecoder emits a decoder that scans a single JSON value and
// hands the raw bytes to (*T).UnmarshalJSON. Invoked when *T satisfies
// json.Unmarshaler. Null is passed through verbatim (matches stdlib, which
// lets the Unmarshaler decide); pointer-level null handling — "set the
// pointer to nil instead of calling UnmarshalJSON" — lives in buildPtrDecoder.
func buildUnmarshalerDecoder(t reflect.Type) typedDecodeFn {
	tt := t
	return func(d *decoder, p unsafe.Pointer) error {
		d.skipWS()
		if d.p >= len(d.data) {
			return syntaxErr("unexpected end", d.p)
		}
		start := d.p
		if err := d.skipValue(); err != nil {
			return err
		}
		raw := d.data[start:d.p]
		// Match encoding/json's contract: UnmarshalJSON implementations must
		// copy data themselves if they retain it after returning. RawMessage
		// already does that, so avoid an unconditional copy here.
		u := reflect.NewAt(tt, p).Interface().(json.Unmarshaler)
		return u.UnmarshalJSON(raw)
	}
}

// buildTextUnmarshalerDecoder is the JSON-string → UnmarshalText path.
// Only accepts a JSON string (or null, which leaves the value zeroed).
func buildTextUnmarshalerDecoder(t reflect.Type) typedDecodeFn {
	tt := t
	return func(d *decoder, p unsafe.Pointer) error {
		d.skipWS()
		if d.p >= len(d.data) {
			return syntaxErr("unexpected end", d.p)
		}
		if d.data[d.p] == 'n' {
			return d.decodeNull()
		}
		if d.data[d.p] != '"' {
			return &UnmarshalTypeError{Value: "non-string", Type: tt, Offset: int64(d.p)}
		}
		raw, err := d.decodeStringRaw()
		if err != nil {
			return err
		}
		u := reflect.NewAt(tt, p).Interface().(encoding.TextUnmarshaler)
		return u.UnmarshalText(raw)
	}
}

// buildMarshalerEncoder emits an encoder that calls MarshalJSON and appends
// the (validated) bytes to the output buffer. ptrRecv selects between value-
// and pointer-receiver dispatch.
func buildMarshalerEncoder(t reflect.Type, ptrRecv bool) typedEncodeFn {
	tt := t
	return func(e *encoder, p unsafe.Pointer) error {
		var iv interface{}
		if ptrRecv {
			iv = reflect.NewAt(tt, p).Interface()
		} else {
			iv = reflect.NewAt(tt, p).Elem().Interface()
		}
		m, ok := iv.(json.Marshaler)
		if !ok {
			return &UnsupportedTypeError{Type: tt}
		}
		raw, err := m.MarshalJSON()
		if err != nil {
			return &MarshalerError{Type: tt, Err: err, sourceFunc: "MarshalJSON"}
		}
		if !Valid(raw) {
			return &MarshalerError{
				Type:       tt,
				Err:        fmt.Errorf("MarshalJSON returned invalid JSON"),
				sourceFunc: "MarshalJSON",
			}
		}
		e.buf = appendCompactEscapedJSON(e.buf, raw)
		return nil
	}
}

// buildTextMarshalerEncoder emits a MarshalText → JSON-string encoder.
func buildTextMarshalerEncoder(t reflect.Type, ptrRecv bool) typedEncodeFn {
	tt := t
	return func(e *encoder, p unsafe.Pointer) error {
		var iv interface{}
		if ptrRecv {
			iv = reflect.NewAt(tt, p).Interface()
		} else {
			iv = reflect.NewAt(tt, p).Elem().Interface()
		}
		m, ok := iv.(encoding.TextMarshaler)
		if !ok {
			return &UnsupportedTypeError{Type: tt}
		}
		text, err := m.MarshalText()
		if err != nil {
			return &MarshalerError{Type: tt, Err: err, sourceFunc: "MarshalText"}
		}
		e.writeString(b2sUnsafe(text))
		return nil
	}
}
