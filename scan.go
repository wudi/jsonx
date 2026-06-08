package jsonx

import "unsafe"

// hasFastScan reports whether the host has a SIMD string-scan kernel
// available. Defined per-arch:
//   - amd64: runtime cpuid check for AVX-512 BW (scan_amd64.go)
//   - arm64: unconditionally true — NEON is mandatory in ARMv8-A
//     (scan_arm64.go)
//   - other arches: false (scan_other.go)
//
// The matching SIMD kernels are scanStringSIMD and skipWSSIMD, whose
// implementations live in the per-arch .s files (AVX-512BW on amd64,
// NEON on arm64). On arches without a kernel, the stubs in
// scan_other.go return 0 and are never reached since hasFastScan is
// false.

// scanString returns the offset of the first byte in p[0:n] that is '"',
// '\\', < 0x20, '<', '>', '&', or >= 0x80. The extra encoder-oriented
// breakpoints are harmless false positives for decode: decode resumes
// scalar scanning at the returned byte and only treats true JSON string
// terminators/escapes/control bytes specially.
//
// Dispatches to the SIMD kernel when hasFastScan is true and n >= 64
// (threshold amortises the broadcast/zeroupper setup cost). Falls back
// to an 8-byte SWAR scan otherwise.
func scanString(p unsafe.Pointer, n int) int {
	if n <= 32 {
		return scanStringTable(p, n)
	}
	if hasFastScan && n >= 64 {
		return scanStringSIMD((*byte)(p), n)
	}
	return scanStringSWAR(p, n)
}

func scanStringTable(p unsafe.Pointer, n int) int {
	for i := 0; i < n; i++ {
		c := *(*byte)(unsafe.Pointer(uintptr(p) + uintptr(i)))
		if !stringSafeSet[c] {
			return i
		}
	}
	return n
}

// scanStringSWAR is the pure-Go fallback. 8 bytes at a time via the
// stringEncodeBreakMask formula.
func scanStringSWAR(p unsafe.Pointer, n int) int {
	i := 0
	for i+8 <= n {
		w := *(*uint64)(unsafe.Pointer(uintptr(p) + uintptr(i)))
		if stringEncodeBreakMask(w) != 0 {
			// precise byte position within this 8-byte window
			for j := 0; j < 8; j++ {
				c := *(*byte)(unsafe.Pointer(uintptr(p) + uintptr(i+j)))
				if !stringSafeSet[c] {
					return i + j
				}
			}
		}
		i += 8
	}
	for i < n {
		c := *(*byte)(unsafe.Pointer(uintptr(p) + uintptr(i)))
		if !stringSafeSet[c] {
			return i
		}
		i++
	}
	return n
}
