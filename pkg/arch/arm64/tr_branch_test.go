package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/vmpacker/pkg/vm"
)

// TestTrBranch_OutOfRange_TailCall 验证越界的无条件 B 被翻译为原生尾调用
// (OpCallNative + OpRet)，而不是报错。
//
// 复现截图中的真实案例:
//
//	偏移 0x0308: B (raw=0x1400B111) → 目标 0x2C74C 超出函数范围 [0, 0x640)
//
// 函数绝对地址 0x31B3F8，故原生尾调用目标 = 0x31B3F8 + 0x2C74C = 0x347B44。
func TestTrBranch_OutOfRange_TailCall(t *testing.T) {
	const (
		funcAddr = uint64(0x31B3F8)
		funcSize = 0x640
		instOff  = 0x308
		rawB     = 0x1400B111
		wantAbs  = uint64(0x347B44)
	)

	d := NewDecoder()
	inst := d.Decode(rawB, instOff)
	expect(t, "decoded Op is B", int(B), inst.Op)

	tr := NewTranslator(funcAddr, funcSize)
	result, err := tr.Translate([]vm.Instruction{inst})
	if err != nil {
		t.Fatalf("Translate returned error: %v", err)
	}

	// 不应产生任何 Unsupported（越界 B 被接受为尾调用）
	expect(t, "no unsupported", 0, len(result.Unsupported))
	// 应记录一条尾调用
	expect(t, "one tail call recorded", 1, len(result.TailCalls))

	// 字节码布局: [OpCallNative][abs64][OpRet][0]...
	bc := result.Bytecode
	if len(bc) < 11 {
		t.Fatalf("bytecode too short: %d", len(bc))
	}
	expect(t, "op[0] = CALL_NATIVE", vm.OpCallNative, bc[0])
	gotAbs := binary.LittleEndian.Uint64(bc[1:9])
	expect(t, "native tail-call target", wantAbs, gotAbs)
	expect(t, "op after call = RET", vm.OpRet, bc[9])
	expect(t, "RET operand reg = 0", byte(0), bc[10])
}

// TestTrBranch_InRange_Jmp 验证函数内的无条件 B 仍翻译为 VM 内部跳转 OpJmp，
// 不走尾调用路径。
func TestTrBranch_InRange_Jmp(t *testing.T) {
	// B .  → raw=0x14000000 (imm26=0)，目标 offset 0 在 [0,0x20) 内，
	// 且 offset 0 处存在真实指令（本指令自身），fixup 可解析。
	d := NewDecoder()
	inst := d.Decode(0x14000000, 0)
	expect(t, "decoded Op is B", int(B), inst.Op)

	tr := NewTranslator(0x400000, 0x20)
	result, err := tr.Translate([]vm.Instruction{inst})
	if err != nil {
		t.Fatalf("Translate returned error: %v", err)
	}

	expect(t, "no tail call for in-range B", 0, len(result.TailCalls))
	expect(t, "op[0] = JMP", vm.OpJmp, result.Bytecode[0])
}
