package main

import (
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
)

// scanStringSIMD(p *byte, n int) int
// Returns the index of the first byte in p[0..n] that is '"' (0x22),
// '\\' (0x5C), < 0x20, '<', '>', '&', or >= 0x80. Returns n if no match.
//
// Uses AVX-512BW: VMOVDQU64 (64-byte loads), VPCMPEQB / VPCMPUB, and
// KMOVQ + TZCNTQ for position-of-first-hit.
func main() {
	TEXT("scanStringSIMD", NOSPLIT, "func(p *byte, n int) int")
	Pragma("noescape")

	p := Load(Param("p"), GP64())
	n := Load(Param("n"), GP64())

	off := GP64()
	XORQ(off, off)

	// Broadcast constants into ZMM1..7.
	tmp := GP64()
	z1, z2, z3, z4, z5, z6, z7 := ZMM(), ZMM(), ZMM(), ZMM(), ZMM(), ZMM(), ZMM()

	MOVQ(U32(0x22), tmp)
	VPBROADCASTB(tmp.As32(), z1) // 0x22 '"'
	MOVQ(U32(0x5c), tmp)
	VPBROADCASTB(tmp.As32(), z2) // 0x5c '\\'
	MOVQ(U32(0x20), tmp)
	VPBROADCASTB(tmp.As32(), z3) // 0x20 (for < compare)
	MOVQ(U32(0x3c), tmp)
	VPBROADCASTB(tmp.As32(), z4) // '<'
	MOVQ(U32(0x3e), tmp)
	VPBROADCASTB(tmp.As32(), z5) // '>'
	MOVQ(U32(0x26), tmp)
	VPBROADCASTB(tmp.As32(), z6) // '&'
	MOVQ(U32(0x80), tmp)
	VPBROADCASTB(tmp.As32(), z7) // 0x80 (for >= compare)

	// Main 64-byte loop.
	Label("loop")
	remain := GP64()
	MOVQ(n, remain)
	SUBQ(off, remain)
	CMPQ(remain, U32(64))
	JL(LabelRef("tail"))

	z0 := ZMM()
	VMOVDQU64(Mem{Base: p, Index: off, Scale: 1}, z0)

	k1, k2, k3, k4 := K(), K(), K(), K()
	VPCMPEQB(z1, z0, k1)        // == 0x22
	VPCMPEQB(z2, z0, k2)        // == 0x5c
	KORQ(k1, k2, k3)            // quote or backslash
	VPCMPUB(Imm(1), z3, z0, k4) // unsigned LT: z0 < 0x20  (imm8=1: LT)
	KORQ(k3, k4, k3)
	VPCMPEQB(z4, z0, k4) // == '<'
	KORQ(k3, k4, k3)
	VPCMPEQB(z5, z0, k4) // == '>'
	KORQ(k3, k4, k3)
	VPCMPEQB(z6, z0, k4) // == '&'
	KORQ(k3, k4, k3)
	VPCMPUB(Imm(5), z7, z0, k4) // unsigned GE: z0 >= 0x80 (imm8=5: NLT)
	KORQ(k3, k4, k3)
	KTESTQ(k3, k3)
	JNZ(LabelRef("found64"))

	ADDQ(U32(64), off)
	JMP(LabelRef("loop"))

	Label("found64")
	// Extract bit index of first set bit.
	bx := GP64()
	KMOVQ(k3, bx)
	TZCNTQ(bx, bx)
	ADDQ(bx, off)
	Store(off, ReturnIndex(0))
	VZEROUPPER()
	RET()

	// Scalar tail for last < 64 bytes.
	Label("tail")
	b := GP8()
	Label("tail_loop")
	CMPQ(off, n)
	JGE(LabelRef("notfound"))
	MOVB(Mem{Base: p, Index: off, Scale: 1}, b)
	CMPB(b, U8(0x22))
	JE(LabelRef("done"))
	CMPB(b, U8(0x5c))
	JE(LabelRef("done"))
	CMPB(b, U8(0x20))
	JB(LabelRef("done"))
	CMPB(b, U8(0x3c))
	JE(LabelRef("done"))
	CMPB(b, U8(0x3e))
	JE(LabelRef("done"))
	CMPB(b, U8(0x26))
	JE(LabelRef("done"))
	CMPB(b, U8(0x80))
	JAE(LabelRef("done"))
	INCQ(off)
	JMP(LabelRef("tail_loop"))

	Label("notfound")
	Label("done")
	Store(off, ReturnIndex(0))
	VZEROUPPER()
	RET()

	Generate()
}
