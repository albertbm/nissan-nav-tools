package consent

// ARM32 encoders for the handful of instruction forms the rewritten handler
// uses. Branch offsets are relative to the instruction's address plus 8,
// which is where the ARM7 pipeline leaves the program counter.

func bl(at, to uint32) uint32  { return 0xeb000000 | (to-(at+8))>>2&0xffffff }
func b(at, to uint32) uint32   { return 0xea000000 | (to-(at+8))>>2&0xffffff }
func bne(at, to uint32) uint32 { return 0x1a000000 | (to-(at+8))>>2&0xffffff }

// movw loads a 16-bit immediate, which plain mov cannot always express.
func movw(rd, imm uint32) uint32 {
	return 0xe3000000 | imm>>12<<16 | rd<<12 | imm&0xfff
}

// movImm is mov Rd, #imm where ARM's 8-bit-plus-even-rotate immediate can hold
// the value, and movw where it cannot.
func movImm(rd, imm uint32) uint32 {
	for rot := uint32(0); rot < 16; rot++ {
		if v := imm<<(2*rot) | imm>>(32-2*rot); v < 0x100 {
			return 0xe3a00000 | rot<<8 | rd<<12 | v
		}
	}
	return movw(rd, imm)
}

// ldrPC is ldr Rd, [pc, #off], the usual way to pick up a constant from the
// literal pool after the function body.
func ldrPC(at, rd, to uint32) uint32 {
	off := to - (at + 8)
	if off > 0xfff {
		panic("patch: literal pool out of reach of ldr")
	}
	return 0xe59f0000 | rd<<12 | off
}
