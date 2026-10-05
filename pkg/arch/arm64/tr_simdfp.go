package arm64

import (
	"fmt"

	"github.com/vmpacker/pkg/vm"
)

// ============================================================
// SIMD&FP 访存 + MOVI 翻译 (数据搬运子集)
//
// V 寄存器 (V0-V31) 直接按序号映射到 VM 的 V[] 寄存器文件。
// 仅支持 offset 形 (WB=0)——典型函数里栈帧由整数序言 (stp x29,x30,[sp,#-N]!)
// 分配, FP 寄存器溢出用偏移寻址, 故 offset 形已覆盖。pre/post 回写形暂报错
// (安全中止, 绝不产出可能错误的回写语义)。
// ============================================================

// vreg 将 ARM64 V 寄存器号映射到 VM V 寄存器号 (直接映射 0-31)。
func (t *Translator) vreg(n int) (byte, error) {
	if n < 0 || n > 31 {
		return 0, fmt.Errorf("V 寄存器 V%d 超出范围", n)
	}
	return byte(n), nil
}

// trVLoadStore 翻译 LDR/STR (SIMD&FP) 单寄存器 (offset 形)。
func (t *Translator) trVLoadStore(inst vm.Instruction, isLoad bool) error {
	if inst.WB != 0 {
		return fmt.Errorf("SIMD&FP 访存回写(pre/post)模式暂不支持")
	}
	vt, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	base, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	op := vm.OpVStore
	if isLoad {
		op = vm.OpVLoad
	}
	t.emit(op, vt, base)
	t.emitU32(uint32(int32(inst.Imm)))
	t.emit(byte(inst.Shift)) // width
	return nil
}

// trVLoadStorePair 翻译 LDP/STP (SIMD&FP) 寄存器对 (offset 形)。
func (t *Translator) trVLoadStorePair(inst vm.Instruction, isLoad bool) error {
	if inst.WB != 0 {
		return fmt.Errorf("SIMD&FP 寄存器对回写(pre/post)模式暂不支持")
	}
	vt1, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	vt2, err := t.vreg(inst.Rm)
	if err != nil {
		return err
	}
	base, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	op := vm.OpVStoreP
	if isLoad {
		op = vm.OpVLoadP
	}
	t.emit(op, vt1, vt2, base)
	t.emitU32(uint32(int32(inst.Imm)))
	t.emit(byte(inst.Shift)) // element width
	return nil
}

// trVMovi 翻译 MOVI/MVNI — 把 128-bit 立即数写入 V 寄存器。
func (t *Translator) trVMovi(inst vm.Instruction) error {
	vt, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	lane := uint64(inst.Imm)
	var lo, hi uint64
	lo = lane
	if inst.Shift == 16 {
		hi = lane // 128-bit: 两个 64-bit 半区相同
	} else {
		hi = 0 // 64-bit: 高 64 位清零
	}
	t.emit(vm.OpVMovi, vt)
	t.emitU64(lo)
	t.emitU64(hi)
	return nil
}
