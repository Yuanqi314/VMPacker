package arm64

import (
	"testing"

	"github.com/vmpacker/pkg/vm"
)

// 真实 JNI_OnLoad 截图里的 SIMD&FP 搬运指令 (objdump 核对过)。
// 字段: Op, Vt(Rd), Vt2(Rm), base(Rn), imm(字节偏移), width(Shift)
func TestDecode_SIMDFP_Movement(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw           uint32
		op            Op
		vt, vt2, base int
		imm           int64
		width         int
	}{
		{0x3DC00100, V_LDR, 0, -1, 8, 0, 16},    // ldr q0, [x8]
		{0x3D803FE0, V_STR, 0, -1, 31, 240, 16}, // str q0, [sp, #240]
		{0x3DC0BF60, V_LDR, 0, -1, 27, 752, 16}, // ldr q0, [x27, #752]
		{0x3C8A83E0, V_STR, 0, -1, 31, 168, 16}, // stur q0, [sp, #168]
		{0x6F00E400, V_MOVI, 0, -1, -1, 0, 16},  // movi v0.2d, #0
		{0xAD0303E0, V_STP, 0, 0, 31, 96, 16},   // stp q0, q0, [sp, #96]
		{0xAD0083E0, V_STP, 0, 0, 31, 16, 16},   // stp q0, q0, [sp, #16]
		{0xFD44E100, V_LDR, 0, -1, 8, 2496, 8},  // ldr d0, [x8, #2496]
		{0xFD0027E0, V_STR, 0, -1, 31, 72, 8},   // str d0, [sp, #72]
		{0xFD001BE1, V_STR, 1, -1, 31, 48, 8},   // str d1, [sp, #48]
	}
	for _, c := range cases {
		in := d.Decode(c.raw, 0)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "Vt", c.vt, in.Rd)
		expect(t, "Vt2", c.vt2, in.Rm)
		expect(t, "base", c.base, in.Rn)
		expect(t, "imm", c.imm, in.Imm)
		expect(t, "width", c.width, in.Shift)
	}
}

// LDP q (load pair) + MVNI 组合, 以及 S 宽度 (ldr s).
func TestDecode_SIMDFP_More(t *testing.T) {
	d := NewDecoder()
	// ldp q0, q1, [sp, #0]  → 0xad400be0
	in := d.Decode(0xAD400BE0, 0)
	expect(t, "LDP op", int(V_LDP), in.Op)
	expect(t, "LDP vt", 0, in.Rd)
	expect(t, "LDP vt2", 2, in.Rm) // Rt2 bits[14:10] = 2 (q2)
	expect(t, "LDP width", 16, in.Shift)
	// ldr s0, [x0]  → 0xbd400000
	in = d.Decode(0xBD400000, 0)
	expect(t, "LDR s op", int(V_LDR), in.Op)
	expect(t, "LDR s width", 4, in.Shift)
}

// FMOV 全形态 (objdump 核对) + VFPExpandImm 精确值。
func TestDecode_SIMDFP_FMOV(t *testing.T) {
	d := NewDecoder()
	expect(t, "fmov s0,s1 Op", int(V_FMOV_VV), d.Decode(0x1E204020, 0).Op)
	expect(t, "fmov d0,d1 width", 8, d.Decode(0x1E604020, 0).Shift)
	expect(t, "fmov s0,w1 Op", int(V_FMOV_GV), d.Decode(0x1E270020, 0).Op)
	expect(t, "fmov d0,x1 Op", int(V_FMOV_GV), d.Decode(0x9E670020, 0).Op)
	expect(t, "fmov w0,s1 Op", int(V_FMOV_VG), d.Decode(0x1E260020, 0).Op)
	expect(t, "fmov x0,d1 Op", int(V_FMOV_VG), d.Decode(0x9E660020, 0).Op)
	// FMOV 立即数 → 精确 IEEE 位
	expect(t, "fmov d0,#1.0", int64(0x3FF0000000000000), d.Decode(0x1E6E1000, 0).Imm)
	expect(t, "fmov s0,#1.0", int64(0x3F800000), d.Decode(0x1E2E1000, 0).Imm)
	expect(t, "fmov d0,#2.0", int64(0x4000000000000000), d.Decode(0x1E601000, 0).Imm)
	expect(t, "fmov d5,#-0.5", int64(-0x4020000000000000), d.Decode(0x1E7C1005, 0).Imm)
}

// 回写 (pre/post) 寻址: WB + 有符号缩放 imm。
func TestDecode_SIMDFP_Writeback(t *testing.T) {
	d := NewDecoder()
	in := d.Decode(0x3CC10C20, 0) // ldr q0,[x1,#16]!
	expect(t, "pre WB", 3, in.WB)
	expect(t, "pre imm", int64(16), in.Imm)
	in = d.Decode(0x3CC10420, 0) // ldr q0,[x1],#16
	expect(t, "post WB", 1, in.WB)
	in = d.Decode(0xADBF07E0, 0) // stp q0,q1,[sp,#-32]!
	expect(t, "stp pre WB", 3, in.WB)
	expect(t, "stp pre imm", int64(-32), in.Imm)
	expect(t, "stp pre width", 16, in.Shift)
}

// 回写翻译: pre-index 应 emit 基址调整(ADD/SUB_IMM) + V 访存。
func TestTranslate_SIMDFP_Writeback(t *testing.T) {
	d := NewDecoder()
	// stp q0,q1,[sp,#-32]!  (pre-index, imm=-32)
	in := d.Decode(0xADBF07E0, 0)
	tr := NewTranslator(0x400000, 0x10)
	res, err := tr.Translate([]vm.Instruction{in})
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	expect(t, "no unsupported", 0, len(res.Unsupported))
	// 首个操作码应是 SUB_IMM (sp -= 32), 其后是 V_STOREP
	expect(t, "first op = SUB_IMM", vm.OpSubImm, res.Bytecode[0])
	foundStP := false
	pc := 0
	for pc < res.CodeLen {
		op := res.Bytecode[pc]
		sz := vm.InstructionSize(op)
		if sz == 0 {
			break
		}
		if op == vm.OpVStoreP {
			foundStP = true
		}
		pc += sz
	}
	expect(t, "emitted V_STOREP", true, foundStP)
}

// 标量浮点运算解码 (objdump 核对)。
func TestDecode_SIMDFP_FPArith(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw  uint32
		op   Op
		w    int // Shift
		imm  int64
		cond int
	}{
		{0x1E222820, V_FADD, 4, 0, 0},    // fadd s0,s1,s2
		{0x1E622820, V_FADD, 8, 0, 0},    // fadd d
		{0x1E223820, V_FSUB, 4, 0, 0},    // fsub s
		{0x1E220820, V_FMUL, 4, 0, 0},    // fmul s
		{0x1E221820, V_FDIV, 4, 0, 0},    // fdiv s
		{0x1E20C020, V_FABS, 4, 0, 0},    // fabs s
		{0x1E214020, V_FNEG, 4, 0, 0},    // fneg s
		{0x1E21C020, V_FSQRT, 4, 0, 0},   // fsqrt s
		{0x1E22C020, V_FCVT_FF, 4, 8, 0}, // fcvt d,s (in=S out=D)
		{0x1E380020, V_FCVT_FS, 4, 4, 0}, // fcvtzs w,s
		{0x9E780020, V_FCVT_FS, 8, 8, 0}, // fcvtzs x,d
		{0x1E390020, V_FCVT_FU, 4, 4, 0}, // fcvtzu w,s
		{0x1E220020, V_FCVT_SF, 4, 4, 0}, // scvtf s,w
		{0x9E620020, V_FCVT_SF, 8, 8, 0}, // scvtf d,x
		{0x1E230020, V_FCVT_UF, 4, 4, 0}, // ucvtf s,w
		{0x1E222020, V_FCMP, 4, 0, 0},    // fcmp s1,s2
		{0x1E202028, V_FCMP, 4, 1, 0},    // fcmp s1,#0.0 (isZero=1)
		{0x1E220C20, V_FCSEL, 4, 0, 0},   // fcsel s,eq
		{0x1E621C20, V_FCSEL, 8, 0, 1},   // fcsel d,ne
	}
	for _, c := range cases {
		in := d.Decode(c.raw, 0)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "width", c.w, in.Shift)
		expect(t, "imm", c.imm, in.Imm)
		expect(t, "cond", c.cond, in.Cond)
	}
}

// FP 融合乘加解码 (FMADD/FMSUB/FNMADD/FNMSUB)。Ra 存于 inst.Imm。
func TestDecode_SIMDFP_FMADD(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw uint32
		op  Op
		w   int
	}{
		{0x1F420C20, V_FMADD, 8},  // fmadd d0,d1,d2,d3
		{0x1F428C20, V_FMSUB, 8},  // fmsub d0,d1,d2,d3
		{0x1F620C20, V_FNMADD, 8}, // fnmadd d0,d1,d2,d3
		{0x1F628C20, V_FNMSUB, 8}, // fnmsub d0,d1,d2,d3
		{0x1F020C20, V_FMADD, 4},  // fmadd s0,s1,s2,s3
		{0x1F028C20, V_FMSUB, 4},  // fmsub s0,s1,s2,s3
		{0x1F220C20, V_FNMADD, 4}, // fnmadd s0,s1,s2,s3
		{0x1F228C20, V_FNMSUB, 4}, // fnmsub s0,s1,s2,s3
	}
	for _, c := range cases {
		in := d.Decode(c.raw, 0)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "width", c.w, in.Shift)
		expect(t, "Rd", 0, in.Rd)
		expect(t, "Rn", 1, in.Rn)
		expect(t, "Rm", 2, in.Rm)
		expect(t, "Ra(Imm)", int64(3), in.Imm)
	}
}

// NEON lane 搬运解码 (DUP/UMOV/SMOV/INS)。imm5 → es+index, imm4/op/Q → 操作。
func TestDecode_SIMDFP_Copy(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw  uint32
		op   Op
		es   int   // Shift
		idx  int64 // Imm (主索引)
		sidx int   // Cond (INS 元素的源索引)
		sf   bool  // Q
	}{
		{0x4E140420, V_DUP_E, 4, 2, 0, true},  // dup v0.4s, v1.s[2]
		{0x0E070420, V_DUP_E, 1, 3, 0, false}, // dup v0.8b, v1.b[3]
		{0x4E180420, V_DUP_E, 8, 1, 0, true},  // dup v0.2d, v1.d[1]
		{0x4E040C20, V_DUP_G, 4, 0, 0, true},  // dup v0.4s, w1
		{0x0E010C20, V_DUP_G, 1, 0, 0, false}, // dup v0.8b, w1
		{0x0E073C20, V_UMOV, 1, 3, 0, false},  // umov w0, v1.b[3]
		{0x4E183C20, V_UMOV, 8, 1, 0, true},   // umov x0, v1.d[1]
		{0x0E072C20, V_SMOV, 1, 3, 0, false},  // smov w0, v1.b[3]
		{0x4E062C20, V_SMOV, 2, 1, 0, true},   // smov x0, v1.h[1]
		{0x4E0C1C20, V_INS_G, 4, 1, 0, true},  // ins v0.s[1], w1
		{0x4E181C20, V_INS_G, 8, 1, 0, true},  // ins v0.d[1], x1
		{0x6E0C4420, V_INS_E, 4, 1, 2, true},  // ins v0.s[1], v1.s[2]
		{0x6E180420, V_INS_E, 8, 1, 0, true},  // ins v0.d[1], v1.d[0]
	}
	for _, c := range cases {
		in := d.Decode(c.raw, 0)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "es", c.es, in.Shift)
		expect(t, "index", c.idx, in.Imm)
		expect(t, "sidx", c.sidx, in.Cond)
		expect(t, "SF(Q)", c.sf, in.SF)
		expect(t, "Rd", 0, in.Rd)
	}
	// DUP 通用 / UMOV 的寄存器号: Rn/Rd 应为 1 / 0
	in := d.Decode(0x4E040C20, 0) // dup v0.4s, w1
	expect(t, "DUP_G Rn", 1, in.Rn)
	in = d.Decode(0x0E073C20, 0) // umov w0, v1.b[3]
	expect(t, "UMOV Rn(V)", 1, in.Rn)
	// ZR 源/目标: dup v0.4s, wzr → Rn=REG_XZR; umov wzr,v1.s[0] → Rd=REG_XZR
	in = d.Decode(0x4E040FE0, 0) // dup v0.4s, wzr (Rn=31)
	expect(t, "DUP_G wzr Rn", vm.REG_XZR, in.Rn)
}

// NEON lane 搬运翻译: 整段应无 UNSUPPORTED, 全部成功翻译。
func TestTranslate_SIMDFP_Copy(t *testing.T) {
	d := NewDecoder()
	raws := []uint32{
		0x4E140420, // dup v0.4s, v1.s[2]
		0x4E040C20, // dup v0.4s, w1
		0x0E073C20, // umov w0, v1.b[3]
		0x4E062C20, // smov x0, v1.h[1]
		0x4E0C1C20, // ins v0.s[1], w1
		0x6E0C4420, // ins v0.s[1], v1.s[2]
		0x4E040FE0, // dup v0.4s, wzr (ZR 源)
	}
	insts := make([]vm.Instruction, len(raws))
	for i, r := range raws {
		insts[i] = d.Decode(r, i*4)
	}
	tr := NewTranslator(0x400000, 0x20)
	res, err := tr.Translate(insts)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	expect(t, "no unsupported", 0, len(res.Unsupported))
	expect(t, "all translated", len(raws), res.TransInsts)
}

// NEON 向量比较解码 (整数 CMxx + 浮点 FCMxx)。
func TestDecode_SIMDFP_Cmp(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw uint32
		op  Op
		es  int
		nb  int64
	}{
		{0x4EA23420, V_VCMGT, 4, 16},  // cmgt v0.4s
		{0x4EA23C20, V_VCMGE, 4, 16},  // cmge v0.4s
		{0x6EA23420, V_VCMHI, 4, 16},  // cmhi v0.4s
		{0x6EA23C20, V_VCMHS, 4, 16},  // cmhs v0.4s
		{0x6EA28C20, V_VCMEQ, 4, 16},  // cmeq v0.4s
		{0x4EA28C20, V_VCMTST, 4, 16}, // cmtst v0.4s
		{0x4E223420, V_VCMGT, 1, 16},  // cmgt v0.16b
		{0x6EE28C20, V_VCMEQ, 8, 16},  // cmeq v0.2d
		{0x4E22E420, V_VFCMEQ, 4, 16}, // fcmeq v0.4s
		{0x6E22E420, V_VFCMGE, 4, 16}, // fcmge v0.4s
		{0x6EA2E420, V_VFCMGT, 4, 16}, // fcmgt v0.4s
		{0x4E62E420, V_VFCMEQ, 8, 16}, // fcmeq v0.2d
		{0x6EE2E420, V_VFCMGT, 8, 16}, // fcmgt v0.2d
	}
	for _, c := range cases {
		in := d.Decode(c.raw, 0)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "es", c.es, in.Shift)
		expect(t, "nbytes", c.nb, in.Imm)
	}
	tr := NewTranslator(0x400000, 0x20)
	insts := make([]vm.Instruction, len(cases))
	for i, c := range cases {
		insts[i] = d.Decode(c.raw, i*4)
	}
	res, err := tr.Translate(insts)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	expect(t, "no unsupported", 0, len(res.Unsupported))
	expect(t, "all translated", len(cases), res.TransInsts)
}

// 向量浮点融合乘加解码 (FMLA/FMLS)。
func TestDecode_SIMDFP_VFma(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw uint32
		op  Op
		es  int
		nb  int64
	}{
		{0x4E22CC20, V_VFMLA, 4, 16}, // fmla v0.4s
		{0x4EA2CC20, V_VFMLS, 4, 16}, // fmls v0.4s
		{0x4E62CC20, V_VFMLA, 8, 16}, // fmla v0.2d
		{0x4EE2CC20, V_VFMLS, 8, 16}, // fmls v0.2d
		{0x0E22CC20, V_VFMLA, 4, 8},  // fmla v0.2s (Q=0)
	}
	insts := make([]vm.Instruction, len(cases))
	for i, c := range cases {
		in := d.Decode(c.raw, i*4)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "es", c.es, in.Shift)
		expect(t, "nbytes", c.nb, in.Imm)
		insts[i] = in
	}
	tr := NewTranslator(0x400000, 0x20)
	res, err := tr.Translate(insts)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	expect(t, "no unsupported", 0, len(res.Unsupported))
	expect(t, "all translated", len(cases), res.TransInsts)
}

// 向量整数↔浮点转换解码 (SCVTF/UCVTF/FCVTZS/FCVTZU)。
func TestDecode_SIMDFP_VCvt(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw uint32
		op  Op
		es  int
		nb  int64
	}{
		{0x4E21D820, V_VSCVTF, 4, 16},  // scvtf v0.4s
		{0x6E21D820, V_VUCVTF, 4, 16},  // ucvtf v0.4s
		{0x4EA1B820, V_VFCVTZS, 4, 16}, // fcvtzs v0.4s
		{0x6EA1B820, V_VFCVTZU, 4, 16}, // fcvtzu v0.4s
		{0x4E61D820, V_VSCVTF, 8, 16},  // scvtf v0.2d
		{0x4EE1B820, V_VFCVTZS, 8, 16}, // fcvtzs v0.2d
		{0x0E21D820, V_VSCVTF, 4, 8},   // scvtf v0.2s (Q=0)
	}
	insts := make([]vm.Instruction, len(cases))
	for i, c := range cases {
		in := d.Decode(c.raw, i*4)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "es", c.es, in.Shift)
		expect(t, "nbytes", c.nb, in.Imm)
		insts[i] = in
	}
	tr := NewTranslator(0x400000, 0x20)
	res, err := tr.Translate(insts)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	expect(t, "no unsupported", 0, len(res.Unsupported))
	expect(t, "all translated", len(cases), res.TransInsts)
}

// FP 运算翻译 emit: 确认生成对应的 VM 操作码。
func TestTranslate_SIMDFP_FPArith(t *testing.T) {
	d := NewDecoder()
	insts := []vm.Instruction{
		d.Decode(0x1E622820, 0), // fadd d0,d1,d2
		d.Decode(0x1E61C020, 4), // fsqrt d0,d1
		d.Decode(0x1E622020, 8), // fcmp d1,d2
	}
	tr := NewTranslator(0x400000, 0x20)
	res, err := tr.Translate(insts)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	expect(t, "no unsupported", 0, len(res.Unsupported))
	expect(t, "all translated", 3, res.TransInsts)
	gotBin, gotUn, gotCmp := false, false, false
	pc := 0
	for pc < res.CodeLen {
		op := res.Bytecode[pc]
		sz := vm.InstructionSize(op)
		if sz == 0 {
			break
		}
		switch op {
		case vm.OpVFBin:
			gotBin = true
		case vm.OpVFUn:
			gotUn = true
		case vm.OpVFCmp:
			gotCmp = true
		}
		pc += sz
	}
	expect(t, "emitted VF_BIN", true, gotBin)
	expect(t, "emitted VF_UN", true, gotUn)
	expect(t, "emitted VF_CMP", true, gotCmp)
}

// 基础 NEON 向量运算解码 (objdump 核对): Op + esize(Shift) + nbytes(Imm)。
func TestDecode_NEON(t *testing.T) {
	d := NewDecoder()
	cases := []struct {
		raw    uint32
		op     Op
		esize  int
		nbytes int64
	}{
		{0x4E228420, V_VADD, 1, 16},  // add v.16b
		{0x4E628420, V_VADD, 2, 16},  // add v.8h
		{0x4EA28420, V_VADD, 4, 16},  // add v.4s
		{0x4EE28420, V_VADD, 8, 16},  // add v.2d
		{0x0E228420, V_VADD, 1, 8},   // add v.8b (Q=0)
		{0x6EA28420, V_VSUB, 4, 16},  // sub v.4s
		{0x4EA29C20, V_VMUL, 4, 16},  // mul v.4s
		{0x4E221C20, V_VAND, 0, 16},  // and
		{0x4EA21C20, V_VORR, 0, 16},  // orr
		{0x6E221C20, V_VEOR, 0, 16},  // eor
		{0x4E621C20, V_VBIC, 0, 16},  // bic
		{0x4EE21C20, V_VORN, 0, 16},  // orn
		{0x6E205820, V_VNOT, 0, 16},  // not
		{0x4EA11C20, V_VORR, 0, 16},  // mov v.16b (ORR alias)
		{0x4E22D420, V_VFADD, 4, 16}, // fadd v.4s
		{0x4E62D420, V_VFADD, 8, 16}, // fadd v.2d
		{0x4EA2D420, V_VFSUB, 4, 16}, // fsub v.4s
		{0x6E62DC20, V_VFMUL, 8, 16}, // fmul v.2d
		{0x6E22FC20, V_VFDIV, 4, 16}, // fdiv v.4s
	}
	for _, c := range cases {
		in := d.Decode(c.raw, 0)
		expect(t, "Op", int(c.op), in.Op)
		expect(t, "esize", c.esize, in.Shift)
		expect(t, "nbytes", c.nbytes, in.Imm)
	}
}

// 验证翻译器 emit: decode→translate→disasm, 确认生成正确的 VM 操作码。
func TestTranslate_SIMDFP_Emit(t *testing.T) {
	d := NewDecoder()
	// movi v0.2d,#0 ; str q0,[x0,#16] ; ldr q1,[x0] ; stp q0,q1,[x0,#32]
	insts := []vm.Instruction{
		d.Decode(0x6F00E400, 0), // movi v0.2d,#0
		d.Decode(0x3D800400, 4), // str q0, [x0, #16]
		d.Decode(0x3DC00001, 8), // ldr q1, [x0]
	}
	tr := NewTranslator(0x400000, 0x20)
	res, err := tr.Translate(insts)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	expect(t, "no unsupported", 0, len(res.Unsupported))
	expect(t, "all translated", 3, res.TransInsts)
	// 字节码应以 V_MOVI(0x9C) 开头
	expect(t, "first op = V_MOVI", vm.OpVMovi, res.Bytecode[0])
	// 扫描字节码确认出现 V_STORE 与 V_LOAD
	foundStore, foundLoad := false, false
	pc := 0
	for pc < res.CodeLen {
		op := res.Bytecode[pc]
		sz := vm.InstructionSize(op)
		if sz == 0 {
			break
		}
		if op == vm.OpVStore {
			foundStore = true
		}
		if op == vm.OpVLoad {
			foundLoad = true
		}
		pc += sz
	}
	expect(t, "emitted V_STORE", true, foundStore)
	expect(t, "emitted V_LOAD", true, foundLoad)
}
