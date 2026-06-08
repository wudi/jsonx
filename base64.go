package jsonx

import (
	"encoding/base64"
	"slices"
)

func base64DecodeBytes(src []byte) ([]byte, error) {
	// encoding/json uses std base64 with padding.
	dst := make([]byte, base64.StdEncoding.DecodedLen(len(src)))
	n, err := base64.StdEncoding.Decode(dst, src)
	return dst[:n], err
}

func base64Encode(dst []byte, src []byte) []byte {
	n := base64.StdEncoding.EncodedLen(len(src))
	start := len(dst)
	dst = slices.Grow(dst, n)
	dst = dst[:start+n]
	base64.StdEncoding.Encode(dst[start:], src)
	return dst
}
