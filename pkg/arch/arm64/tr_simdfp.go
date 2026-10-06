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

// emitBaseAdj 把基址寄存器按有符号立即数调整 (writeback 用): R[base] += imm。
// 复用整数 ADD/SUB 立即数操作码 (h_add_imm/h_sub_imm 作用于完整 64 位)。
func (t *Translator) emitBaseAdj(base byte, imm int64) {
	if imm >= 0 {
		t.emit(vm.OpAddImm, base, base)
		t.emitU32(uint32(imm))
	} else {
		t.emit(vm.OpSubImm, base, base)
		t.emitU32(uint32(-imm))
	}
}

// trVLoadStore 翻译 LDR/STR (SIMD&FP) 单寄存器。
// 支持 offset / pre-index(WB=3) / post-index(WB=1)。
func (t *Translator) trVLoadStore(inst vm.Instruction, isLoad bool) error {
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
	emitV := func(off int64) {
		t.emit(op, vt, base)
		t.emitU32(uint32(int32(off)))
		t.emit(byte(inst.Shift)) // width
	}
	switch inst.WB {
	case 0: // offset
		emitV(inst.Imm)
	case 3: // pre-index: base += imm, 再访问 [base]
		t.emitBaseAdj(base, inst.Imm)
		emitV(0)
	case 1: // post-index: 访问 [base], 再 base += imm
		emitV(0)
		t.emitBaseAdj(base, inst.Imm)
	default:
		return fmt.Errorf("不支持的 SIMD&FP 访存寻址模式 WB=%d", inst.WB)
	}
	return nil
}

// trVLoadStorePair 翻译 LDP/STP (SIMD&FP) 寄存器对。
// 支持 offset / pre-index(WB=3) / post-index(WB=1)。
func (t *Translator) trVLoadStorePair(inst vm.Instruction, isLoad bool) error {
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
	emitV := func(off int64) {
		t.emit(op, vt1, vt2, base)
		t.emitU32(uint32(int32(off)))
		t.emit(byte(inst.Shift)) // element width
	}
	switch inst.WB {
	case 0:
		emitV(inst.Imm)
	case 3: // pre-index
		t.emitBaseAdj(base, inst.Imm)
		emitV(0)
	case 1: // post-index
		emitV(0)
		t.emitBaseAdj(base, inst.Imm)
	default:
		return fmt.Errorf("不支持的 SIMD&FP 寄存器对寻址模式 WB=%d", inst.WB)
	}
	return nil
}

// trVMov 翻译 FMOV 寄存器搬运 (vec↔vec / gpr↔vec)。
// dir: 0=vec→vec, 1=gpr→vec, 2=vec→gpr。
func (t *Translator) trVMov(inst vm.Instruction, dir byte, dstIsV, srcIsV bool) error {
	var dst, src byte
	var err error
	if dstIsV {
		dst, err = t.vreg(inst.Rd)
	} else {
		dst, err = t.mapReg(inst.Rd)
	}
	if err != nil {
		return err
	}
	if srcIsV {
		src, err = t.vreg(inst.Rn)
	} else {
		src, err = t.mapReg(inst.Rn)
	}
	if err != nil {
		return err
	}
	t.emit(vm.OpVMov, dir, dst, src, byte(inst.Shift))
	return nil
}

// trVFBin 翻译 FP 二元运算 (FADD/FSUB/FMUL/FDIV)。subop: 0+ 1- 2* 3/。
func (t *Translator) trVFBin(inst vm.Instruction, subop byte) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	m, err := t.vreg(inst.Rm)
	if err != nil {
		return err
	}
	t.emit(vm.OpVFBin, subop, d, n, m, byte(inst.Shift))
	return nil
}

// trVFMAdd 翻译 FP 融合乘加 (FMADD/FMSUB/FNMADD/FNMSUB)。
// subop: 0 madd(a+n*m) 1 msub(a-n*m) 2 nmadd(-a-n*m) 3 nmsub(-a+n*m)。
// 累加寄存器 Ra 存放在 inst.Imm。
func (t *Translator) trVFMAdd(inst vm.Instruction, subop byte) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	m, err := t.vreg(inst.Rm)
	if err != nil {
		return err
	}
	a, err := t.vreg(int(inst.Imm))
	if err != nil {
		return err
	}
	t.emit(vm.OpVFMAdd, subop, d, n, m, a, byte(inst.Shift))
	return nil
}

// vnbytes: Q (inst.SF) → 向量总字节 (16=full / 8=half)
func vnbytes(inst vm.Instruction) byte {
	if inst.SF {
		return 16
	}
	return 8
}

// trVDupElem 翻译 DUP Vd.T, Vn.Ts[index] (元素复制到各 lane)。
func (t *Translator) trVDupElem(inst vm.Instruction) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpVDupElem, d, n, byte(inst.Shift), byte(inst.Imm), vnbytes(inst))
	return nil
}

// trVDupGen 翻译 DUP Vd.T, Rn (GPR 复制到各 lane)。
func (t *Translator) trVDupGen(inst vm.Instruction) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	if inst.Rn == vm.REG_XZR {
		t.sPushImm32(0)
		t.sVstore(rn) // R16 = 0 (ZR 源)
	}
	t.emit(vm.OpVDupGen, d, rn, byte(inst.Shift), vnbytes(inst))
	return nil
}

// trVMovToR 翻译 UMOV/SMOV Rd, Vn.Ts[index] (向量 lane → GPR)。sign: 0 零扩展 1 符号扩展。
func (t *Translator) trVMovToR(inst vm.Instruction, sign byte) error {
	d, err := t.mapReg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	sf := byte(0)
	if inst.SF {
		sf = 1
	}
	t.emit(vm.OpVMovToR, d, n, byte(inst.Shift), byte(inst.Imm), sign, sf)
	return nil
}

// trVInsGen 翻译 INS Vd.Ts[index], Rn (GPR → 向量 lane)。
func (t *Translator) trVInsGen(inst vm.Instruction) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	if inst.Rn == vm.REG_XZR {
		t.sPushImm32(0)
		t.sVstore(rn) // R16 = 0 (ZR 源)
	}
	t.emit(vm.OpVInsGen, d, rn, byte(inst.Shift), byte(inst.Imm))
	return nil
}

// trVInsElem 翻译 INS Vd.Ts[didx], Vn.Ts[sidx] (向量 lane → 向量 lane)。
func (t *Translator) trVInsElem(inst vm.Instruction) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpVInsElem, d, n, byte(inst.Shift), byte(inst.Imm), byte(inst.Cond))
	return nil
}

// trVFUn 翻译 FP 一元运算 (FABS/FNEG/FSQRT)。subop: 0 abs 1 neg 2 sqrt。
func (t *Translator) trVFUn(inst vm.Instruction, subop byte) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpVFUn, subop, d, n, byte(inst.Shift))
	return nil
}

// trVFCvt 翻译转换。kind: 0 f→f, 1 f→s, 2 f→u, 3 s→f, 4 u→f。
// dstIsV/srcIsV 指明目标/源是否为向量寄存器 (否则为 GPR)。
func (t *Translator) trVFCvt(inst vm.Instruction, kind byte, dstIsV, srcIsV bool) error {
	var d, n byte
	var err error
	if dstIsV {
		d, err = t.vreg(inst.Rd)
	} else {
		d, err = t.mapReg(inst.Rd)
	}
	if err != nil {
		return err
	}
	if srcIsV {
		n, err = t.vreg(inst.Rn)
	} else {
		n, err = t.mapReg(inst.Rn)
	}
	if err != nil {
		return err
	}
	t.emit(vm.OpVFCvt, kind, d, n, byte(inst.Shift), byte(inst.Imm)) // inw=Shift, outw=Imm
	return nil
}

// trVFCmp 翻译 FCMP (置 FL)。
func (t *Translator) trVFCmp(inst vm.Instruction) error {
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	var m byte
	if inst.Imm == 0 { // 非零比较才有 Rm
		m, err = t.vreg(inst.Rm)
		if err != nil {
			return err
		}
	}
	t.emit(vm.OpVFCmp, n, m, byte(inst.Shift), byte(inst.Imm))
	return nil
}

// trVFCsel 翻译 FCSEL (d = cond ? n : m)。
func (t *Translator) trVFCsel(inst vm.Instruction) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	m, err := t.vreg(inst.Rm)
	if err != nil {
		return err
	}
	t.emit(vm.OpVFCsel, d, n, m, byte(inst.Cond), byte(inst.Shift))
	return nil
}

// --- 基础 NEON 向量运算 ---

func (t *Translator) trVecBin(inst vm.Instruction, subop byte) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecBin, subop, d, n, m, byte(inst.Shift), byte(inst.Imm)) // esize, nbytes
	return nil
}

func (t *Translator) trVecFBin(inst vm.Instruction, subop byte) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecFBin, subop, d, n, m, byte(inst.Shift), byte(inst.Imm))
	return nil
}

func (t *Translator) trVecLogic(inst vm.Instruction, subop byte) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecLogic, subop, d, n, m, byte(inst.Imm)) // nbytes
	return nil
}

// trVecCmp 翻译整数向量比较。subop: 0 EQ 1 GT 2 GE 3 HI 4 HS 5 TST。
func (t *Translator) trVecCmp(inst vm.Instruction, subop byte) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecCmp, subop, d, n, m, byte(inst.Shift), byte(inst.Imm)) // esize, nbytes
	return nil
}

// trVecFCmp 翻译浮点向量比较。subop: 0 EQ 1 GE 2 GT。
func (t *Translator) trVecFCmp(inst vm.Instruction, subop byte) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecFCmp, subop, d, n, m, byte(inst.Shift), byte(inst.Imm))
	return nil
}

// trVecFma 翻译向量浮点融合乘加。subop: 0 FMLA(d+=n*m) 1 FMLS(d-=n*m)。
func (t *Translator) trVecFma(inst vm.Instruction, subop byte) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecFma, subop, d, n, m, byte(inst.Shift), byte(inst.Imm))
	return nil
}

// trVecCvt 翻译向量整数↔浮点转换。subop: 0 SCVTF 1 UCVTF 2 FCVTZS 3 FCVTZU。
func (t *Translator) trVecCvt(inst vm.Instruction, subop byte) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecCvt, subop, d, n, byte(inst.Shift), byte(inst.Imm))
	return nil
}

// trVecPerm 翻译向量置换。subop: 0 ZIP1 1 ZIP2 2 UZP1 3 UZP2 4 TRN1 5 TRN2。
func (t *Translator) trVecPerm(inst vm.Instruction, subop byte) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecPerm, subop, d, n, m, byte(inst.Shift), byte(inst.Imm))
	return nil
}

// trVecRev 翻译元素反转 (REV16/32/64)。container 存于 inst.Cond, esize 存于 inst.Shift。
func (t *Translator) trVecRev(inst vm.Instruction) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecRev, d, n, byte(inst.Cond), byte(inst.Shift), byte(inst.Imm))
	return nil
}

// trVecExt 翻译 EXT (从 concat(Vn:Vm) 的第 index 字节起取 nbytes)。
func (t *Translator) trVecExt(inst vm.Instruction) error {
	d, n, m, err := t.vreg3(inst)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecExt, d, n, m, byte(inst.Shift), byte(inst.Imm))
	return nil
}

func (t *Translator) trVecNot(inst vm.Instruction) error {
	d, err := t.vreg(inst.Rd)
	if err != nil {
		return err
	}
	n, err := t.vreg(inst.Rn)
	if err != nil {
		return err
	}
	t.emit(vm.OpVecNot, d, n, byte(inst.Imm))
	return nil
}

// vreg3 映射 Rd/Rn/Rm 三个向量寄存器。
func (t *Translator) vreg3(inst vm.Instruction) (d, n, m byte, err error) {
	if d, err = t.vreg(inst.Rd); err != nil {
		return
	}
	if n, err = t.vreg(inst.Rn); err != nil {
		return
	}
	m, err = t.vreg(inst.Rm)
	return
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
