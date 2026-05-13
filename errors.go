package jsonx

import (
	"fmt"
	"reflect"
)

type SyntaxError struct {
	Msg    string
	Offset int64
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("jsonx: %s at offset %d", e.Msg, e.Offset)
}

type UnmarshalTypeError struct {
	Value  string
	Type   reflect.Type
	Offset int64
	Struct string // name of the outermost struct containing the field, if any
	Field  string // dotted path of the field within that struct, if any
}

func (e *UnmarshalTypeError) Error() string {
	if e.Struct != "" || e.Field != "" {
		return fmt.Sprintf("jsonx: cannot unmarshal %s into Go struct field %s.%s of type %s",
			e.Value, e.Struct, e.Field, e.Type)
	}
	return fmt.Sprintf("jsonx: cannot unmarshal %s into Go value of type %s", e.Value, e.Type)
}

type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "jsonx: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Ptr {
		return fmt.Sprintf("jsonx: Unmarshal(non-pointer %s)", e.Type)
	}
	return fmt.Sprintf("jsonx: Unmarshal(nil %s)", e.Type)
}

type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return fmt.Sprintf("jsonx: unsupported type: %s", e.Type)
}

func syntaxErr(msg string, off int) error {
	return &SyntaxError{Msg: msg, Offset: int64(off)}
}
