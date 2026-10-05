package arm64

import "github.com/vmpacker/pkg/vm"

// ============================================================
// SIMD&FP 访存 + MOVI 解码 (数据搬运子集)
//
// 仅覆盖"数据搬运"类 SIMD&FP 指令——它们不做向量/浮点运算，
// 只在 V 寄存器与内存/立即数之间搬数据，因此可以安全虚拟化：
//   - LDR/STR (SIMD&FP) 单寄存器: unsigned offset / unscaled(STUR) / pre / post
//   - LDP/STP (SIMD&FP) 寄存器对: signed offset / pre / post
//   - MOVI/MVNI (AdvSIMD 立即数): 将 V 寄存器设为立即数
// 宽度: B(1)/H(2)/S(4)/D(8)/Q(16)
//
// 字段约定 (复用 vm.Instruction):
//   Rd=Vt, Rm=Vt2(pair), Rn=base, Imm=字节偏移, Shift=宽度(字节),
//   WB=寻址模式(0=offset, 1=post, 3=pre)
// MOVI: Rd=Vt, Imm=展开后的 64-bit lane 值, Shift=datasize 字节(8 或 16)
// ============================================================

// fpWidthLoad 由 size:opc 推导 (宽度字节, 是否 load)。ok=false 表示非法组合。
func fpWidthLoad(size, opc int64) (width int, isLoad bool, ok bool) {
	switch size {
	case 0:
		if opc == 2 || opc == 3 {
			return 16, opc == 3, true // Q: opc=10 STR, 11 LDR
		}
		return 1, opc&1 == 1, true // B: opc=00 STR, 01 LDR
	case 1:
		return 2, opc&1 == 1, true // H
	case 2:
		return 4, opc&1 == 1, true // S
	case 3:
		return 8, opc&1 == 1, true // D
	}
	return 0, false, false
}

var simdfpPatterns = []InstrPattern{
	// ---- LDR/STR (SIMD&FP) unsigned offset: size 111 V=1 01 opc imm12 Rn Rt ----
	{
		Name: "V_LDST_UOFF", Mask: 0x3F000000, Value: 0x3D000000, Op: V_LDR,
		Fields: []FieldDef{
			{Name: "size", Hi: 31, Lo: 30},
			{Name: "opc", Hi: 23, Lo: 22},
			{Name: "imm12", Hi: 21, Lo: 10},
			fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w, ld, ok := fpWidthLoad(f["size"], f["opc"])
			if !ok {
				inst.Op = int(UNSUPPORTED)
				return
			}
			inst.Op = int(V_STR)
			if ld {
				inst.Op = int(V_LDR)
			}
			inst.Shift = w
			inst.Imm = f["imm12"] * int64(w) // unsigned offset 按宽度缩放
			inst.WB = 0
		},
	},

	// ---- LDR/STR (SIMD&FP) 非 unsigned-offset: size 111 V=1 00 opc 0 imm9 mode Rn Rt ----
	// mode(bits[11:10]): 00=unscaled(STUR/LDUR), 01=post, 11=pre
	{
		Name: "V_LDST_IDX", Mask: 0x3F200000, Value: 0x3C000000, Op: V_LDR,
		Fields: []FieldDef{
			{Name: "size", Hi: 31, Lo: 30},
			{Name: "opc", Hi: 23, Lo: 22},
			{Name: "imm9", Hi: 20, Lo: 12, Signed: true},
			{Name: "mode", Hi: 11, Lo: 10},
			fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w, ld, ok := fpWidthLoad(f["size"], f["opc"])
			if !ok {
				inst.Op = int(UNSUPPORTED)
				return
			}
			switch f["mode"] {
			case 0:
				inst.WB = 0 // unscaled STUR/LDUR
			case 1:
				inst.WB = 1 // post-index
			case 3:
				inst.WB = 3 // pre-index
			default:
				inst.Op = int(UNSUPPORTED) // mode=10 对 FP 非法
				return
			}
			inst.Op = int(V_STR)
			if ld {
				inst.Op = int(V_LDR)
			}
			inst.Shift = w
			inst.Imm = f["imm9"]
		},
	},

	// ---- LDP/STP (SIMD&FP): opc 101 V=1 cat L imm7 Rt2 Rn Rt ----
	// bits[29:25]=10110; opc(31:30)=00 S / 01 D / 10 Q; cat(25:23) 000/001/010/011
	{
		Name: "V_LDSTP", Mask: 0x3E000000, Value: 0x2C000000, Op: V_STP,
		Fields: []FieldDef{
			{Name: "opc2", Hi: 31, Lo: 30},
			{Name: "cat", Hi: 25, Lo: 23},
			{Name: "L", Hi: 22, Lo: 22},
			{Name: "imm7", Hi: 21, Lo: 15, Signed: true},
			{Name: "Rm", Hi: 14, Lo: 10}, // Rt2
			fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			var w int
			switch f["opc2"] {
			case 0:
				w = 4 // S
			case 1:
				w = 8 // D
			case 2:
				w = 16 // Q
			default:
				inst.Op = int(UNSUPPORTED)
				return
			}
			switch f["cat"] {
			case 0b001:
				inst.WB = 1 // post
			case 0b011:
				inst.WB = 3 // pre
			default:
				inst.WB = 0 // 000(非临时)/010(offset) 当作 offset
			}
			inst.Op = int(V_STP)
			if f["L"] != 0 {
				inst.Op = int(V_LDP)
			}
			inst.Shift = w
			inst.Imm = f["imm7"] * int64(w) // 按宽度缩放
		},
	},

	// ---- MOVI/MVNI (AdvSIMD 立即数): 0 Q op 0 1111 00000 abc cmode 01 defgh Rd ----
	{
		Name: "V_MOVI", Mask: 0x9FF80400, Value: 0x0F000400, Op: V_MOVI,
		Fields: []FieldDef{
			{Name: "Qb", Hi: 30, Lo: 30},
			{Name: "opb", Hi: 29, Lo: 29},
			{Name: "abc", Hi: 18, Lo: 16},
			{Name: "cmode", Hi: 15, Lo: 12},
			{Name: "defgh", Hi: 9, Lo: 5},
			fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			imm8 := uint64(f["abc"]<<5) | uint64(f["defgh"])
			val, ok := advSIMDExpandImm(f["opb"] != 0, int(f["cmode"]), imm8)
			if !ok {
				inst.Op = int(UNSUPPORTED) // ORR/BIC/FMOV 立即数 (非纯搬运) 不支持
				return
			}
			inst.Op = int(V_MOVI)
			inst.Imm = int64(val)
			if f["Qb"] != 0 {
				inst.Shift = 16 // 128-bit: lane 值复制到两个 64-bit 半区
			} else {
				inst.Shift = 8 // 64-bit: 低 64 = lane 值, 高 64 = 0
			}
		},
	},

	// ---- FMOV Vd, Vn (寄存器搬运): 0001 1110 0 ftype 1 000000 10000 Rn Rd ----
	{
		Name: "V_FMOV_RR", Mask: 0xFF3FFC00, Value: 0x1E204000, Op: V_FMOV_VV,
		Fields: []FieldDef{{Name: "ftype", Hi: 23, Lo: 22}, fRn, fRd},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			inst.Shift = fpTypeWidth(f["ftype"])
			if inst.Shift == 0 {
				inst.Op = int(UNSUPPORTED)
			}
		},
	},

	// ---- FMOV (通用 int↔fp): sf 0011110 ftype 1 rmode=00 opcode=11x 000000 Rn Rd ----
	// bit16: 1 = GPR→Vec (opcode 111), 0 = Vec→GPR (opcode 110)
	{
		Name: "V_FMOV_GEN", Mask: 0x7F3EFC00, Value: 0x1E260000, Op: V_FMOV_GV,
		Fields: []FieldDef{{Name: "ftype", Hi: 23, Lo: 22}, {Name: "dir", Hi: 16, Lo: 16}, fRn, fRd},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			inst.Shift = fpTypeWidth(f["ftype"])
			if inst.Shift == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			if f["dir"] != 0 {
				inst.Op = int(V_FMOV_GV) // GPR → Vec
			} else {
				inst.Op = int(V_FMOV_VG) // Vec → GPR
			}
		},
	},

	// ---- FMOV Vd, #imm (标量浮点立即数): 000 11110 0 ftype 1 imm8 100 00000 Rd ----
	{
		Name: "V_FMOV_IMM", Mask: 0xFE201FE0, Value: 0x1E201000, Op: V_FMOV_I,
		Fields: []FieldDef{{Name: "ftype", Hi: 23, Lo: 22}, {Name: "imm8", Hi: 20, Lo: 13}, fRd},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w := fpTypeWidth(f["ftype"])
			if w == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			inst.Shift = w
			inst.Imm = int64(vfpExpandImm(uint64(f["imm8"]), w == 8))
		},
	},
}

// fpTypeWidth: ftype(00=S,01=D,11=H) → 字节宽度 (H 暂不支持返回 0)
func fpTypeWidth(ftype int64) int {
	switch ftype {
	case 0:
		return 4 // S
	case 1:
		return 8 // D
	}
	return 0 // H (ftype=11) 等暂不支持
}

// vfpExpandImm 实现 ARM ARM VFPExpandImm: 8-bit 浮点立即数 → IEEE 位模式。
// is64=true 返回 double 的 64 位; false 返回 float 的 32 位 (零扩展到 64)。
func vfpExpandImm(imm8 uint64, is64 bool) uint64 {
	var e, f uint
	if is64 {
		e, f = 11, 52
	} else {
		e, f = 8, 23
	}
	sign := (imm8 >> 7) & 1
	b6 := (imm8 >> 6) & 1
	var notb6 uint64
	if b6 == 0 {
		notb6 = 1
	}
	var rep uint64 // Replicate(imm8<6>, e-3)
	if b6 != 0 {
		rep = (uint64(1) << (e - 3)) - 1
	}
	exp := (notb6 << (e - 1)) | (rep << 2) | ((imm8 >> 4) & 3)
	frac := (imm8 & 0xF) << (f - 4)
	return (sign << (e + f)) | (exp << f) | frac
}

// advSIMDExpandImm 实现 ARM ARM 的 AdvSIMDExpandImm，返回 64-bit lane 值。
// 仅支持 MOVI/MVNI (纯"设寄存器"语义)；ORR/BIC/FMOV 立即数返回 ok=false。
func advSIMDExpandImm(op bool, cmode int, imm8 uint64) (uint64, bool) {
	rep8 := func(b uint64) uint64 { return b * 0x0101010101010101 }
	rep16 := func(h uint64) uint64 { h &= 0xFFFF; return h | h<<16 | h<<32 | h<<48 }
	rep32 := func(w uint64) uint64 { w &= 0xFFFFFFFF; return w | w<<32 }

	var v uint64
	switch cmode >> 1 {
	case 0b000:
		v = rep32(imm8)
	case 0b001:
		v = rep32(imm8 << 8)
	case 0b010:
		v = rep32(imm8 << 16)
	case 0b011:
		v = rep32(imm8 << 24)
	case 0b100:
		v = rep16(imm8)
	case 0b101:
		v = rep16(imm8 << 8)
	case 0b110:
		if cmode&1 == 0 {
			v = rep32((imm8 << 8) | 0xFF) // MSL #8
		} else {
			v = rep32((imm8 << 16) | 0xFFFF) // MSL #16
		}
	case 0b111:
		if cmode&1 == 1 {
			// cmode=1111: FMOV 向量立即数 — 非纯搬运, 不支持
			return 0, false
		}
		if !op {
			v = rep8(imm8) // MOVI .16B
		} else {
			// MOVI .2D: imm8 每个 bit 扩展为一个字节 (0x00/0xFF)
			var d uint64
			for i := 0; i < 8; i++ {
				if imm8&(1<<uint(i)) != 0 {
					d |= uint64(0xFF) << uint(i*8)
				}
			}
			return d, true // 2D 形永远是 MOVI (op=1 在此处不表示 MVNI)
		}
	}

	// cmode 最低位为 1 (且非 110x) → ORR/BIC 立即数 (修改型), 不支持
	if (cmode&1) == 1 && (cmode>>2) != 0b11 {
		return 0, false
	}
	// op=1 (cmode 非 1110) → MVNI: 按位取反
	if op {
		v = ^v
	}
	return v, true
}
