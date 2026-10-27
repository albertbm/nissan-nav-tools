package consent

// ARM32 encoders for the rewritten handler; branch offsets are relative to the instruction address plus 8 (ARM7 pipeline PC).

func bl(at, to uint32) uint32  { return 0xeb000000 | (to-(at+8))>>2&0xffffff }
func b(at, to uint32) uint32   { return 0xea000000 | (to-(at+8))>>2&0xffffff }
func bne(at, to uint32) uint32 { return 0x1a000000 | (to-(at+8))>>2&0xffffff }

func movw(rd, imm uint32) uint32 {
	return 0xe3000000 | imm>>12<<16 | rd<<12 | imm&0xfff
}

// movImm is mov Rd, #imm where ARM's 8-bit-plus-even-rotate immediate can hold the value, movw otherwise.
func movImm(rd, imm uint32) uint32 {
	for rot := uint32(0); rot < 16; rot++ {
		if v := imm<<(2*rot) | imm>>(32-2*rot); v < 0x100 {
			return 0xe3a00000 | rot<<8 | rd<<12 | v
		}
	}
	return movw(rd, imm)
}

func ldrPC(at, rd, to uint32) uint32 {
	off := to - (at + 8)
	if off > 0xfff {
		panic("patch: literal pool out of reach of ldr")
	}
	return 0xe59f0000 | rd<<12 | off
}
