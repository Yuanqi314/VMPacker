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

	// ---- 通用 int↔fp 组: sf 0011110 ftype 1 rmode opcode 000000 Rn Rd ----
	// 覆盖 FMOV(gpr↔vec) + FCVTZS/FCVTZU(浮点→整数) + SCVTF/UCVTF(整数→浮点)
	{
		Name: "V_INT_FP", Mask: 0x7F20FC00, Value: 0x1E200000, Op: V_FMOV_GV,
		Fields: []FieldDef{
			{Name: "sf", Hi: 31, Lo: 31},
			{Name: "ftype", Hi: 23, Lo: 22},
			{Name: "rmode", Hi: 20, Lo: 19},
			{Name: "opcode", Hi: 18, Lo: 16},
			fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			fpw := fpTypeWidth(f["ftype"])
			if fpw == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			intw := 4
			if f["sf"] != 0 {
				intw = 8
			}
			rmode, opc := f["rmode"], f["opcode"]
			switch {
			case rmode == 0 && opc == 0b111: // FMOV gpr→vec
				inst.Op, inst.Shift = int(V_FMOV_GV), fpw
			case rmode == 0 && opc == 0b110: // FMOV vec→gpr
				inst.Op, inst.Shift = int(V_FMOV_VG), fpw
			case rmode == 0b11 && opc == 0b000: // FCVTZS 浮点→有符号整数
				inst.Op, inst.Shift, inst.Imm = int(V_FCVT_FS), fpw, int64(intw)
			case rmode == 0b11 && opc == 0b001: // FCVTZU 浮点→无符号整数
				inst.Op, inst.Shift, inst.Imm = int(V_FCVT_FU), fpw, int64(intw)
			case rmode == 0 && opc == 0b010: // SCVTF 有符号整数→浮点
				inst.Op, inst.Shift, inst.Imm = int(V_FCVT_SF), intw, int64(fpw)
			case rmode == 0 && opc == 0b011: // UCVTF 无符号整数→浮点
				inst.Op, inst.Shift, inst.Imm = int(V_FCVT_UF), intw, int64(fpw)
			default:
				inst.Op = int(UNSUPPORTED) // 其它舍入模式转换暂不支持
			}
		},
	},

	// ---- FP 二元运算: 0001 1110 0 ftype 1 Rm opcode 10 Rn Rd ----
	// opcode[15:12]: 0000 FMUL, 0001 FDIV, 0010 FADD, 0011 FSUB
	{
		Name: "V_FP_2SRC", Mask: 0xFF200C00, Value: 0x1E200800, Op: V_FADD,
		Fields: []FieldDef{
			{Name: "ftype", Hi: 23, Lo: 22},
			fRm16, {Name: "opcode", Hi: 15, Lo: 12}, fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w := fpTypeWidth(f["ftype"])
			if w == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			inst.Shift = w
			switch f["opcode"] {
			case 0b0000:
				inst.Op = int(V_FMUL)
			case 0b0001:
				inst.Op = int(V_FDIV)
			case 0b0010:
				inst.Op = int(V_FADD)
			case 0b0011:
				inst.Op = int(V_FSUB)
			default:
				inst.Op = int(UNSUPPORTED) // FMAX/FMIN/FNMUL 等暂不支持
			}
		},
	},

	// ---- FP 一元运算 (FABS/FNEG/FSQRT/FCVT): 0001 1110 0 ftype 1 opcode 10000 Rn Rd ----
	// 注: FMOV(opcode=000000) 由上面 V_FMOV_RR 先行匹配
	{
		Name: "V_FP_1SRC", Mask: 0xFF207C00, Value: 0x1E204000, Op: V_FABS,
		Fields: []FieldDef{
			{Name: "ftype", Hi: 23, Lo: 22},
			{Name: "opcode", Hi: 20, Lo: 15}, fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w := fpTypeWidth(f["ftype"])
			if w == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			inst.Shift = w
			switch f["opcode"] {
			case 0b000001:
				inst.Op = int(V_FABS)
			case 0b000010:
				inst.Op = int(V_FNEG)
			case 0b000011:
				inst.Op = int(V_FSQRT)
			case 0b000100: // FCVT → S (目标单精度)
				inst.Op, inst.Imm = int(V_FCVT_FF), 4
			case 0b000101: // FCVT → D (目标双精度)
				inst.Op, inst.Imm = int(V_FCVT_FF), 8
			default:
				inst.Op = int(UNSUPPORTED) // FRINTx 等暂不支持
			}
		},
	},

	// ---- FCMP: 0001 1110 0 ftype 1 Rm 00 1000 Rn opc2 ----
	// opc2[4:0]: 00000 FCMP, 01000 FCMP #0.0 (bit3=1 表示与 0.0 比较)
	{
		Name: "V_FCMP", Mask: 0xFF20FC00, Value: 0x1E202000, Op: V_FCMP,
		Fields: []FieldDef{
			{Name: "ftype", Hi: 23, Lo: 22},
			fRm16, {Name: "Rn", Hi: 9, Lo: 5}, {Name: "opc2", Hi: 4, Lo: 0},
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w := fpTypeWidth(f["ftype"])
			if w == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			inst.Shift = w
			if f["opc2"]&0b01000 != 0 {
				inst.Imm = 1 // 与 #0.0 比较
			}
		},
	},

	// ---- FCSEL: 0001 1110 0 ftype 1 Rm cond 11 Rn Rd ----
	{
		Name: "V_FCSEL", Mask: 0xFF200C00, Value: 0x1E200C00, Op: V_FCSEL,
		Fields: []FieldDef{
			{Name: "ftype", Hi: 23, Lo: 22},
			fRm16, {Name: "cond", Hi: 15, Lo: 12}, fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w := fpTypeWidth(f["ftype"])
			if w == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			inst.Shift = w
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

	// ---- AdvSIMD copy: 0 Q op 01110000 imm5 0 imm4 1 Rn Rd ----
	// 覆盖 DUP(元素/通用) / UMOV / SMOV / INS(通用/元素)
	{
		Name: "V_ASIMD_COPY", Mask: 0x9FE08400, Value: 0x0E000400, Op: V_DUP_E,
		Fields: []FieldDef{
			{Name: "Q", Hi: 30, Lo: 30}, {Name: "op", Hi: 29, Lo: 29},
			{Name: "imm5", Hi: 20, Lo: 16}, {Name: "imm4", Hi: 14, Lo: 11},
			fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			es, index := decodeImm5(f["imm5"])
			if es == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			Q, op, imm4 := f["Q"], f["op"], f["imm4"]
			inst.Shift = es
			inst.SF = Q == 1
			inst.Imm = int64(index)
			if op == 1 {
				// INS (element): Q 必为 1; imm4 = 源索引 (按 log2(es) 右移)
				if Q != 1 {
					inst.Op = int(UNSUPPORTED)
					return
				}
				inst.Op = int(V_INS_E)
				inst.Cond = int(imm4 >> log2Esize(es)) // 源索引
				return
			}
			switch imm4 {
			case 0b0000: // DUP (element): Rn/Rd 均为 V 寄存器
				inst.Op = int(V_DUP_E)
			case 0b0001: // DUP (general): Rn 为 GPR (31=ZR)
				inst.Op = int(V_DUP_G)
				xzrReplace(&inst.Rn)
			case 0b0101: // SMOV: Rd 为 GPR (31=ZR)
				inst.Op = int(V_SMOV)
				xzrReplace(&inst.Rd)
			case 0b0111: // UMOV: Rd 为 GPR (31=ZR)
				inst.Op = int(V_UMOV)
				xzrReplace(&inst.Rd)
			case 0b0011: // INS (general): Q 必为 1, Rn 为 GPR (31=ZR)
				if Q != 1 {
					inst.Op = int(UNSUPPORTED)
					return
				}
				inst.Op = int(V_INS_G)
				xzrReplace(&inst.Rn)
			default:
				inst.Op = int(UNSUPPORTED)
			}
		},
	},

	// ---- FP 融合乘加 (3-source): 0001 1111 0 ftype o1 Rm o0 Ra Rn Rd ----
	// (o1,o0): (0,0) FMADD  (0,1) FMSUB  (1,0) FNMADD  (1,1) FNMSUB
	{
		Name: "V_FP_3SRC", Mask: 0xFF000000, Value: 0x1F000000, Op: V_FMADD,
		Fields: []FieldDef{
			{Name: "ftype", Hi: 23, Lo: 22},
			{Name: "o1", Hi: 21, Lo: 21}, fRm16,
			{Name: "o0", Hi: 15, Lo: 15}, {Name: "ra", Hi: 14, Lo: 10}, fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			w := fpTypeWidth(f["ftype"])
			if w == 0 {
				inst.Op = int(UNSUPPORTED)
				return
			}
			inst.Shift = w
			inst.Imm = f["ra"] // 累加寄存器 Ra
			switch (f["o1"] << 1) | f["o0"] {
			case 0b00:
				inst.Op = int(V_FMADD)
			case 0b01:
				inst.Op = int(V_FMSUB)
			case 0b10:
				inst.Op = int(V_FNMADD)
			case 0b11:
				inst.Op = int(V_FNMSUB)
			}
		},
	},

	// ---- ASIMD 3-same: 0 Q U 01110 size 1 Rm opcode 1 Rn Rd ----
	// 覆盖整数 ADD/SUB/MUL、逻辑 AND/BIC/ORR/ORN/EOR、浮点 FADD/FSUB/FMUL/FDIV
	{
		Name: "V_ASIMD_3SAME", Mask: 0x9F200400, Value: 0x0E200400, Op: V_VADD,
		Fields: []FieldDef{
			{Name: "Q", Hi: 30, Lo: 30}, {Name: "U", Hi: 29, Lo: 29},
			{Name: "b23", Hi: 23, Lo: 23}, {Name: "b22", Hi: 22, Lo: 22},
			{Name: "opcode", Hi: 15, Lo: 11}, fRm16, fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			Q, U := f["Q"], f["U"]
			b23, b22, opc := f["b23"], f["b22"], f["opcode"]
			if Q != 0 {
				inst.Imm = 16 // nbytes (full)
			} else {
				inst.Imm = 8 // half
			}
			fpEs := 4
			if b22 != 0 {
				fpEs = 8
			}
			size := (b23 << 1) | b22 // 整数/逻辑用
			switch {
			case U == 0 && b23 == 0 && opc == 0b11010:
				inst.Op, inst.Shift = int(V_VFADD), fpEs
			case U == 0 && b23 == 1 && opc == 0b11010:
				inst.Op, inst.Shift = int(V_VFSUB), fpEs
			case U == 1 && b23 == 0 && opc == 0b11011:
				inst.Op, inst.Shift = int(V_VFMUL), fpEs
			case U == 1 && b23 == 0 && opc == 0b11111:
				inst.Op, inst.Shift = int(V_VFDIV), fpEs
			case opc == 0b10000: // ADD/SUB (整数, 按 lane)
				inst.Shift = 1 << uint(size)
				if U == 0 {
					inst.Op = int(V_VADD)
				} else {
					inst.Op = int(V_VSUB)
				}
			case opc == 0b10011 && U == 0: // MUL
				inst.Op, inst.Shift = int(V_VMUL), 1<<uint(size)
			case opc == 0b11001 && U == 0 && b23 == 0: // FMLA
				inst.Op, inst.Shift = int(V_VFMLA), fpEs
			case opc == 0b11001 && U == 0 && b23 == 1: // FMLS
				inst.Op, inst.Shift = int(V_VFMLS), fpEs
			case opc == 0b11100 && U == 0 && b23 == 0: // FCMEQ
				inst.Op, inst.Shift = int(V_VFCMEQ), fpEs
			case opc == 0b11100 && U == 1 && b23 == 0: // FCMGE
				inst.Op, inst.Shift = int(V_VFCMGE), fpEs
			case opc == 0b11100 && U == 1 && b23 == 1: // FCMGT
				inst.Op, inst.Shift = int(V_VFCMGT), fpEs
			case opc == 0b00110: // CMGT (U=0) / CMHI (U=1)
				inst.Shift = 1 << uint(size)
				if U == 0 {
					inst.Op = int(V_VCMGT)
				} else {
					inst.Op = int(V_VCMHI)
				}
			case opc == 0b00111: // CMGE (U=0) / CMHS (U=1)
				inst.Shift = 1 << uint(size)
				if U == 0 {
					inst.Op = int(V_VCMGE)
				} else {
					inst.Op = int(V_VCMHS)
				}
			case opc == 0b10001: // CMTST (U=0) / CMEQ (U=1)
				inst.Shift = 1 << uint(size)
				if U == 0 {
					inst.Op = int(V_VCMTST)
				} else {
					inst.Op = int(V_VCMEQ)
				}
			case opc == 0b00011: // 逻辑 (按 U,size)
				switch {
				case U == 0 && size == 0b00:
					inst.Op = int(V_VAND)
				case U == 0 && size == 0b01:
					inst.Op = int(V_VBIC)
				case U == 0 && size == 0b10:
					inst.Op = int(V_VORR)
				case U == 0 && size == 0b11:
					inst.Op = int(V_VORN)
				case U == 1 && size == 0b00:
					inst.Op = int(V_VEOR)
				default:
					inst.Op = int(UNSUPPORTED) // BSL/BIT/BIF 暂不支持
				}
			default:
				inst.Op = int(UNSUPPORTED)
			}
		},
	},

	// ---- ASIMD 2-reg misc: NOT/MVN ----
	{
		Name: "V_NOT", Mask: 0xBFFFFC00, Value: 0x2E205800, Op: V_VNOT,
		Fields: []FieldDef{{Name: "Q", Hi: 30, Lo: 30}, fRn, fRd},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			if f["Q"] != 0 {
				inst.Imm = 16
			} else {
				inst.Imm = 8
			}
		},
	},

	// ---- ASIMD 置换: ZIP1/ZIP2/UZP1/UZP2/TRN1/TRN2 ----
	// 0 Q 001110 size 0 Rm 0 opcode 10 Rn Rd  (opcode[14:12] 区分)
	{
		Name: "V_ASIMD_PERM", Mask: 0xBF208C00, Value: 0x0E000800, Op: V_ZIP1,
		Fields: []FieldDef{
			{Name: "Q", Hi: 30, Lo: 30}, {Name: "size", Hi: 23, Lo: 22},
			{Name: "opcode", Hi: 14, Lo: 12}, fRm16, fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			if f["Q"] != 0 {
				inst.Imm = 16
			} else {
				inst.Imm = 8
			}
			inst.Shift = 1 << uint(f["size"]) // 元素字节
			switch f["opcode"] {
			case 0b001:
				inst.Op = int(V_UZP1)
			case 0b010:
				inst.Op = int(V_TRN1)
			case 0b011:
				inst.Op = int(V_ZIP1)
			case 0b101:
				inst.Op = int(V_UZP2)
			case 0b110:
				inst.Op = int(V_TRN2)
			case 0b111:
				inst.Op = int(V_ZIP2)
			default:
				inst.Op = int(UNSUPPORTED)
			}
		},
	},

	// ---- ASIMD EXT: 0 Q 101110 00 0 Rm 0 imm4 0 Rn Rd ----
	{
		Name: "V_ASIMD_EXT", Mask: 0xBFE08400, Value: 0x2E000000, Op: V_EXT,
		Fields: []FieldDef{
			{Name: "Q", Hi: 30, Lo: 30}, fRm16,
			{Name: "imm4", Hi: 14, Lo: 11}, fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			nb := int64(8)
			if f["Q"] != 0 {
				nb = 16
			}
			inst.Imm = nb
			inst.Shift = int(f["imm4"]) // 提取起始字节
			// Q=0 时 imm4 的高位必须为 0 (否则是 UNDEFINED), 这里不额外校验
		},
	},

	// ---- ASIMD 2-reg misc: 整数↔浮点转换 + 元素反转 REV16/32/64 ----
	// 0 Q U 01110 size 10000 opcode 10 Rn Rd  (宽解码, 非目标 opcode → UNSUPPORTED)
	//   SCVTF/UCVTF (11101), FCVTZS/FCVTZU (11011), REV16/64 (00000/00001)
	// 必须排在 V_NOT 之后 (NOT 先匹配其专属编码)。
	{
		Name: "V_ASIMD_2MISC", Mask: 0x9F3E0C00, Value: 0x0E200800, Op: V_VSCVTF,
		Fields: []FieldDef{
			{Name: "Q", Hi: 30, Lo: 30}, {Name: "U", Hi: 29, Lo: 29},
			{Name: "size", Hi: 23, Lo: 22}, {Name: "opcode", Hi: 16, Lo: 12},
			fRn, fRd,
		},
		Post: func(f map[string]int64, inst *vm.Instruction) {
			U, size, opc := f["U"], f["size"], f["opcode"]
			if f["Q"] != 0 {
				inst.Imm = 16
			} else {
				inst.Imm = 8
			}
			switch {
			case opc == 0b11101 && U == 0:
				inst.Op, inst.Shift = int(V_VSCVTF), cvtEs(size)
			case opc == 0b11101 && U == 1:
				inst.Op, inst.Shift = int(V_VUCVTF), cvtEs(size)
			case opc == 0b11011 && U == 0:
				inst.Op, inst.Shift = int(V_VFCVTZS), cvtEs(size)
			case opc == 0b11011 && U == 1:
				inst.Op, inst.Shift = int(V_VFCVTZU), cvtEs(size)
			case opc == 0b00000 && U == 0: // REV64: container 8 字节
				inst.Op, inst.Shift, inst.Cond = int(V_REV64), 1<<uint(size), 8
			case opc == 0b00000 && U == 1: // REV32: container 4 字节
				inst.Op, inst.Shift, inst.Cond = int(V_REV32), 1<<uint(size), 4
			case opc == 0b00001 && U == 0: // REV16: container 2 字节
				inst.Op, inst.Shift, inst.Cond = int(V_REV16), 1<<uint(size), 2
			default:
				inst.Op = int(UNSUPPORTED)
			}
		},
	},
}

// cvtEs: 2-reg-misc 转换指令的 size(bit22) → 元素字节 (0→S/4, 1→D/8)
func cvtEs(size int64) int {
	if size&1 != 0 {
		return 8
	}
	return 4
}

// decodeImm5 解析 AdvSIMD copy 的 imm5: 最低置位决定元素大小, 其余位为索引。
// 返回 es=0 表示保留/不支持的编码。
func decodeImm5(imm5 int64) (es int, index int) {
	switch {
	case imm5&1 == 1: // xxxx1 → B (1 字节)
		return 1, int(imm5 >> 1)
	case imm5&3 == 2: // xxx10 → H (2 字节)
		return 2, int(imm5 >> 2)
	case imm5&7 == 4: // xx100 → S (4 字节)
		return 4, int(imm5 >> 3)
	case imm5&15 == 8: // x1000 → D (8 字节)
		return 8, int(imm5 >> 4)
	}
	return 0, 0
}

// log2Esize: 元素字节 (1/2/4/8) → log2 (0/1/2/3)
func log2Esize(es int) int {
	n := 0
	for e := es; e > 1; e >>= 1 {
		n++
	}
	return n
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
