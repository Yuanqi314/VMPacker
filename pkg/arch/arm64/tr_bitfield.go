package arm64

import (
	"github.com/vmpacker/pkg/vm"
)

// ============================================================
// 位域翻译 — SBFM (安全，无 temp 寄存器冲突)
// UBFM 已迁移到 tr_stack.go (trStackUBFM)
// ============================================================

func (t *Translator) trSBFM(inst vm.Instruction) error {
	rd, err := t.mapReg(inst.Rd)
	if err != nil {
		return err
	}
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	immr := uint32(inst.Imm)
	imms := uint32(inst.Shift)

	regSize := uint32(32)
	if inst.SF {
		regSize = 64
	}

	if imms == regSize-1 {
		// ASR: 对于32-bit，先trunc32确保高32位为0，再用64-bit ASR
		if !inst.SF {
			// 先将源值符号扩展到64位：SHL 32, ASR 32 使bit31扩展到bit63
			t.emit(vm.OpShlImm, rd, rn)
			t.emitU32(32)
			t.emit(vm.OpAsrImm, rd, rd)
			t.emitU32(32 + immr)
			t.trunc32(rd)
		} else {
			t.emit(vm.OpAsrImm, rd, rn)
			t.emitU32(immr)
		}
		return nil
	}
	if immr == 0 {
		// SXTB/SXTH/SXTW: 符号扩展
		// VM寄存器是64-bit，所以需要用64-bit的shift宽度来做sign extension
		var shiftAmt uint32
		if inst.SF {
			shiftAmt = 64 - (imms + 1)
		} else {
			// 32-bit: 先SHL到bit63位置，再ASR回来，最后trunc32
			shiftAmt = 64 - (imms + 1)
		}
		t.emit(vm.OpShlImm, rd, rn)
		t.emitU32(shiftAmt)
		t.emit(vm.OpAsrImm, rd, rd)
		t.emitU32(shiftAmt)
		if !inst.SF {
			t.trunc32(rd)
		}
		return nil
	}
	// 通用 SBFM: SBFX (imms>=immr) / SBFIZ (imms<immr)。
	// 统一用 "左移顶对齐 + 算术右移做符号扩展" 在 64 位里实现, 32 位结尾 trunc32。
	if imms >= immr {
		// SBFX: 取 bits[imms:immr] 符号扩展到 bit0
		l := 63 - imms
		r := l + immr
		t.emit(vm.OpShlImm, rd, rn)
		t.emitU32(l)
		t.emit(vm.OpAsrImm, rd, rd)
		t.emitU32(r)
	} else {
		// SBFIZ: 取 bits[imms:0] (width=imms+1) 符号扩展, 左移 lsb 放到高位
		width := imms + 1
		lsb := uint32(64) - immr
		if !inst.SF {
			lsb = 32 - immr
		}
		t.emit(vm.OpShlImm, rd, rn)
		t.emitU32(64 - width)
		t.emit(vm.OpAsrImm, rd, rd)
		t.emitU32(64 - width)
		t.emit(vm.OpShlImm, rd, rd)
		t.emitU32(lsb)
	}
	if !inst.SF {
		t.trunc32(rd)
	}
	return nil
}
