package arm64

import (
	"fmt"

	"github.com/vmpacker/pkg/vm"
)

// ============================================================
// 分支翻译 — B / B.cond / BL / BLR / BR / TBZ
// CSEL/CBZ 已迁移到 tr_stack.go (trStackCSEL/trStackCBZ)
// ============================================================

func (t *Translator) trBranch(inst vm.Instruction) error {
	target := inst.Offset + int(inst.Imm)

	if target < 0 || target > t.funcSize {
		// 目标落在函数体之外 —— 这是一次跨函数的直接跳转。无条件 B 在
		// ARM64 中是终结性控制转移，编译器通常把以下几种情况生成为 B 而非 BL：
		//   - 尾调用           return foo(args)  →  B foo
		//   - noreturn 调用     __stack_chk_fail / abort
		//   - 冷块外联          .text.unlikely 里的分支
		// 这些都等价于“以当前参数寄存器(X0-X7)转移到目标、且不再返回本函数”。
		// 因此翻译为：原生调用绝对目标 + RET 返回其结果。对真正的尾调用，
		// 返回值在 X0、控制权回到原始调用者，语义一致；对 noreturn 调用，
		// native 调用不会返回，后面的 RET 为死代码（无害）。
		//
		// 这与寄存器间接跳转 BR Xn 在解释器中的既有处理一致
		// (stub h_br_reg：目标在函数外时按 native 尾调用处理)，只是对直接 B
		// 在翻译期即可确定目标，故在此处静态展开。
		//
		// 仅限无条件 B；条件分支 (B.cond/CBZ/TBZ) 的越界目标仍按错误处理，
		// 因为它们越界更可能是“函数大小判定过小”而非真正的尾调用。
		absTarget := uint64(int64(t.funcAddr) + int64(inst.Offset) + inst.Imm)
		t.emit(vm.OpCallNative)
		t.emitU64(absTarget)
		t.emit(vm.OpRet, 0)
		t.tailCalls = append(t.tailCalls, fmt.Sprintf(
			"偏移 0x%04X: B → 原生尾调用 0x%X (跨函数直接跳转)",
			inst.Offset, absTarget))
		return nil
	}

	t.emit(vm.OpJmp)
	fixPos := t.pos()
	t.emitU32(0)
	t.fixups = append(t.fixups, branchFixup{vmOffset: fixPos, arm64Target: target})
	return nil
}

func (t *Translator) trBranchCond(inst vm.Instruction) error {
	target := inst.Offset + int(inst.Imm)

	if target < 0 || target > t.funcSize {
		return fmt.Errorf("条件分支目标 0x%X 超出函数范围 [0, 0x%X]", target, t.funcSize)
	}

	var vmOp byte
	switch inst.Cond {
	case COND_EQ:
		vmOp = vm.OpJe
	case COND_NE:
		vmOp = vm.OpJne
	case COND_LT:
		vmOp = vm.OpJl
	case COND_GE:
		vmOp = vm.OpJge
	case COND_GT:
		vmOp = vm.OpJgt
	case COND_LE:
		vmOp = vm.OpJle
	case COND_CS:
		vmOp = vm.OpJae
	case COND_CC:
		vmOp = vm.OpJb
	case COND_HI:
		vmOp = vm.OpJa
	case COND_LS:
		vmOp = vm.OpJbe
	case COND_MI:
		vmOp = vm.OpJl // MI: N==1 → FL_SIGN set
	case COND_PL:
		vmOp = vm.OpJge // PL: N==0 → FL_SIGN not set
	default:
		return fmt.Errorf("不支持的条件码 0x%X", inst.Cond)
	}

	t.emit(vmOp)
	fixPos := t.pos()
	t.emitU32(0)
	t.fixups = append(t.fixups, branchFixup{vmOffset: fixPos, arm64Target: target})
	return nil
}

func (t *Translator) trBL(inst vm.Instruction) error {
	target := uint64(int64(t.funcAddr) + int64(inst.Offset) + inst.Imm)

	t.emit(vm.OpCallNative)
	t.emitU64(target)
	return nil
}

func (t *Translator) trBLR(inst vm.Instruction) error {
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpCallReg, rn)
	return nil
}

func (t *Translator) trBR(inst vm.Instruction) error {
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpBrReg, rn)
	return nil
}

// trTBZ 翻译 TBZ/TBNZ — test bit and branch
// 字节码: [OpTbz/OpTbnz][reg][bit][target32] = 7B
// inst.Shift = bit number (b5:b40), inst.Imm = offset (已乘4)
func (t *Translator) trTBZ(inst vm.Instruction, isZero bool) error {
	target := inst.Offset + int(inst.Imm)

	if target < 0 || target > t.funcSize {
		return fmt.Errorf("TBZ/TBNZ 分支目标 0x%X 超出函数范围 [0, 0x%X)", target, t.funcSize)
	}

	rd, err := t.mapReg(inst.Rd)
	if err != nil {
		return err
	}

	var vmOp byte
	if isZero {
		vmOp = vm.OpTbz
	} else {
		vmOp = vm.OpTbnz
	}

	t.emit(vmOp, rd, byte(inst.Shift))
	fixPos := t.pos()
	t.emitU32(0)
	t.fixups = append(t.fixups, branchFixup{vmOffset: fixPos, arm64Target: target})
	return nil
}
