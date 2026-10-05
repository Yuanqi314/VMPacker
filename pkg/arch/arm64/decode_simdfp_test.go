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
