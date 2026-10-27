package consent

// build holds one map version's addresses; an unrecognised build is refused.
type build struct {
	name   string
	modLen int

	codeROM int // start of ScreenStateCC's code in the MTCP segment's plaintext (the NK2 ROM image)
	codeVA  uint32

	getContext uint32
	setVar     uint32
	getState   uint32
	getState2  uint32
	evHandler  uint32
	execFn     uint32

	skip handler
}

// handler is SK_CWS_AGREE_EXECUTE: stock is its body as shipped, compared before writing so a moved build is refused.
type handler struct {
	va    uint32
	tail  uint32
	pTrue uint32

	varSkip    uint32
	varAgree   uint32
	varStartup uint32

	action uint32
	ctrl   uint32
	notify uint32

	stock []uint32
}

var builds = []build{
	{
		name:    "G218ENNI.120",
		modLen:  31984640,
		codeROM: 0xa7c000,
		codeVA:  0x413d1000,

		getContext: 0x41b2f560,
		setVar:     0x41b2f3d0,
		getState:   0x41b2f430,
		getState2:  0x41b2f3f0,
		evHandler:  0x41b2f640,
		execFn:     0x41b2f5b0,

		skip: handler{
			va:    0x41a0a5c0,
			tail:  0x41a0a680,
			pTrue: 0x41a0a690,

			varSkip:    73,
			varAgree:   257,
			varStartup: 628,

			action: 4892,
			ctrl:   25,
			notify: 1687,

			stock: []uint32{
				0xeb0493e6, 0xe58d0134, 0xe59f20bc, 0xe3a01049, 0xe59d0134,
				0xeb04937d, 0xeb0493e0, 0xe58d0138, 0xe59d0138, 0xeb049391,
				0xe58d013c, 0xe301131c, 0xe59d013c, 0xeb049411, 0xe58d0140,
				0xe59d3140, 0xe3530000, 0x1a000007, 0xeb0493d4, 0xe58d0144,
				0xe59d0144, 0xeb049375, 0xe58d0148, 0xe301131c, 0xe59d0148,
				0xeb049405, 0xe3a03001, 0xe58d3000, 0xea000012,
			},
		},
	},
	{
		name:    "G214ELNI.062",
		modLen:  30647808,
		codeROM: 0xa11000,
		codeVA:  0x412e1000,

		getContext: 0x419d376c,
		setVar:     0x419d35dc,
		getState:   0x419d363c,
		getState2:  0x419d35fc,
		evHandler:  0x419d384c,
		execFn:     0x419d37bc,

		skip: handler{
			va:    0x418b6488,
			tail:  0x418b6530,
			pTrue: 0x418b6540,

			varSkip:    47,
			varAgree:   223,
			varStartup: 563,

			action: 4676,
			ctrl:   25,
			notify: 1609,

			stock: []uint32{
				0xeb0474b7, 0xe58d00d4, 0xe59f20a4, 0xe3a0102f, 0xe59d00d4,
				0xeb04744e, 0xeb0474b1, 0xe58d00d8, 0xe59d00d8, 0xeb047462,
				0xe58d00dc, 0xe3011244, 0xe59d00dc, 0xeb0474e2, 0xe58d00e0,
				0xe59d30e0, 0xe3530000, 0x1a000007, 0xeb0474a5, 0xe58d00e4,
				0xe59d00e4, 0xeb047446, 0xe58d00e8, 0xe3011244, 0xe59d00e8,
				0xeb0474d6, 0xe3a03001, 0xe58d3000, 0xea00000c,
			},
		},
	},
	{
		name:       "G214ELNI.017",
		modLen:     30675968,
		codeROM:    0xa11000,
		codeVA:     0x412e1000,
		getContext: 0x419d4c38,
		setVar:     0x419d4aa8,
		getState:   0x419d4b08,
		getState2:  0x419d4ac8,
		evHandler:  0x419d4d18,
		execFn:     0x419d4c88,
		skip: handler{
			va:         0x418b7e78,
			tail:       0x418b7f20,
			pTrue:      0x418b7f30,
			varSkip:    47,
			varAgree:   224,
			varStartup: 565,
			action:     4678,
			ctrl:       25,
			notify:     1611,
			stock: []uint32{
				0xeb04736e, 0xe58d00d4, 0xe59f20a4, 0xe3a0102f, 0xe59d00d4,
				0xeb047305, 0xeb047368, 0xe58d00d8, 0xe59d00d8, 0xeb047319,
				0xe58d00dc, 0xe3011246, 0xe59d00dc, 0xeb047399, 0xe58d00e0,
				0xe59d30e0, 0xe3530000, 0x1a000007, 0xeb04735c, 0xe58d00e4,
				0xe59d00e4, 0xeb0472fd, 0xe58d00e8, 0xe3011246, 0xe59d00e8,
				0xeb04738d, 0xe3a03001, 0xe58d3000, 0xea00000c,
			},
		},
	},
}

// skipBody writes VAR_CWS_AGREE_SKIP and both agree-status variables, then notifies Telema: 29 instructions in, 29 out.
func (bd build) skipBody() []uint32 {
	p := bd.skip
	a := p.va
	done := a + 0x68
	return []uint32{
		bl(a+0x00, bd.getContext),
		ldrPC(a+0x04, 2, p.pTrue),
		movImm(1, p.varSkip),
		bl(a+0x0c, bd.setVar),
		bl(a+0x10, bd.getContext),
		ldrPC(a+0x14, 2, p.pTrue),
		movImm(1, p.varAgree),
		bl(a+0x1c, bd.setVar),
		bl(a+0x20, bd.getContext),
		ldrPC(a+0x24, 2, p.pTrue),
		movImm(1, p.varStartup),
		bl(a+0x2c, bd.setVar),
		bl(a+0x30, bd.getContext),
		movw(2, p.notify),
		movImm(1, p.ctrl),
		bl(a+0x3c, bd.execFn),
		bl(a+0x40, bd.getContext),
		bl(a+0x44, bd.getState),
		movw(1, p.action),
		bl(a+0x4c, bd.evHandler),
		0xe3500000,
		bne(a+0x54, done),
		bl(a+0x58, bd.getContext),
		bl(a+0x5c, bd.getState2),
		movw(1, p.action),
		bl(a+0x64, bd.evHandler),
		0xe3a03001,
		0xe58d3000,
		b(a+0x70, p.tail),
	}
}
