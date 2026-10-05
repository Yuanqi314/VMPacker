/*
 * h_vsimd.h — SIMD&FP 寄存器访存 + MOVI handler (数据搬运子集)
 *
 * 操作 128-bit V 寄存器文件 vm->V[0..31]。只搬数据, 不做运算。
 *   OP_VLOAD  [op][vt][base][imm32][width]        8B  V[vt] ← *(R[base]+imm)
 *   OP_VSTORE [op][vt][base][imm32][width]        8B  *(R[base]+imm) ← V[vt]
 *   OP_VLOADP [op][vt1][vt2][base][imm32][width]  9B  成对加载
 *   OP_VSTOREP[op][vt1][vt2][base][imm32][width]  9B  成对存储
 *   OP_VMOVI  [op][vt][val_lo64][val_hi64]        18B V[vt] ← 128-bit 立即数
 *
 * 当 base 为 SP(R[31]) 时做 vm_stk 边界检查, 越界静默跳过 (与 GPR 访存一致)。
 * 非 SP base 持有真实指针 (如 X0-X7/X8 结构体指针), 直接解引用。
 * 逐字节拷贝避免对齐问题。
 */
#ifndef H_VSIMD_H
#define H_VSIMD_H

#include "../vm_decode.h"
#include "../vm_types.h"
#include "h_mem.h" /* VM_STK_CHECK */

/* LDR Vt, [Xb, #imm] */
static inline u32 h_vload(vm_ctx_t *vm) {
  u8 vt = vm->bc[vm->pc + 1], base = vm->bc[vm->pc + 2];
  i32 off = (i32)rd32(&vm->bc[vm->pc + 3]);
  u8 w = vm->bc[vm->pc + 7];
  u64 addr = vm->R[base & 31] + (i64)off;
  if ((base & 31) == 31 && !VM_STK_CHECK(vm, addr, w))
    return 8; /* SP 越界, 跳过 */
  u8 *dst = vm->V[vt & 31];
  for (int i = 0; i < 16; i++)
    dst[i] = 0; /* 高位清零 (width<16 时 V 的高字节置 0) */
  const u8 *src = (const u8 *)addr;
  for (int i = 0; i < w; i++)
    dst[i] = src[i];
  return 8;
}

/* STR Vt, [Xb, #imm] */
static inline u32 h_vstore(vm_ctx_t *vm) {
  u8 vt = vm->bc[vm->pc + 1], base = vm->bc[vm->pc + 2];
  i32 off = (i32)rd32(&vm->bc[vm->pc + 3]);
  u8 w = vm->bc[vm->pc + 7];
  u64 addr = vm->R[base & 31] + (i64)off;
  if ((base & 31) == 31 && !VM_STK_CHECK(vm, addr, w))
    return 8;
  const u8 *src = vm->V[vt & 31];
  u8 *d = (u8 *)addr;
  for (int i = 0; i < w; i++)
    d[i] = src[i];
  return 8;
}

/* LDP Vt1, Vt2, [Xb, #imm] */
static inline u32 h_vloadp(vm_ctx_t *vm) {
  u8 vt1 = vm->bc[vm->pc + 1], vt2 = vm->bc[vm->pc + 2], base = vm->bc[vm->pc + 3];
  i32 off = (i32)rd32(&vm->bc[vm->pc + 4]);
  u8 w = vm->bc[vm->pc + 8];
  u64 a1 = vm->R[base & 31] + (i64)off, a2 = a1 + w;
  if ((base & 31) == 31 && !VM_STK_CHECK(vm, a1, (u32)w * 2))
    return 9;
  u8 *d1 = vm->V[vt1 & 31], *d2 = vm->V[vt2 & 31];
  for (int i = 0; i < 16; i++) {
    d1[i] = 0;
    d2[i] = 0;
  }
  const u8 *s1 = (const u8 *)a1, *s2 = (const u8 *)a2;
  for (int i = 0; i < w; i++) {
    d1[i] = s1[i];
    d2[i] = s2[i];
  }
  return 9;
}

/* STP Vt1, Vt2, [Xb, #imm] */
static inline u32 h_vstorep(vm_ctx_t *vm) {
  u8 vt1 = vm->bc[vm->pc + 1], vt2 = vm->bc[vm->pc + 2], base = vm->bc[vm->pc + 3];
  i32 off = (i32)rd32(&vm->bc[vm->pc + 4]);
  u8 w = vm->bc[vm->pc + 8];
  u64 a1 = vm->R[base & 31] + (i64)off, a2 = a1 + w;
  if ((base & 31) == 31 && !VM_STK_CHECK(vm, a1, (u32)w * 2))
    return 9;
  const u8 *s1 = vm->V[vt1 & 31], *s2 = vm->V[vt2 & 31];
  u8 *p1 = (u8 *)a1, *p2 = (u8 *)a2;
  for (int i = 0; i < w; i++) {
    p1[i] = s1[i];
    p2[i] = s2[i];
  }
  return 9;
}

/* FMOV 寄存器搬运  [5B: op | dir | dst | src | width]
 * dir=0 vec→vec, 1 gpr→vec, 2 vec→gpr。只搬 width 字节, 目标高位清零。 */
static inline u32 h_vmov(vm_ctx_t *vm) {
  u8 dir = vm->bc[vm->pc + 1], dst = vm->bc[vm->pc + 2];
  u8 src = vm->bc[vm->pc + 3], w = vm->bc[vm->pc + 4];
  if (dir == 2) { /* vec → gpr: 零扩展 */
    const u8 *s = vm->V[src & 31];
    u64 v = 0;
    for (int i = 0; i < w && i < 8; i++)
      v |= (u64)s[i] << (i * 8);
    vm->R[dst & 31] = v;
  } else {
    u8 *d = vm->V[dst & 31];
    for (int i = 0; i < 16; i++)
      d[i] = 0;
    if (dir == 1) { /* gpr → vec */
      u64 v = vm->R[src & 31];
      for (int i = 0; i < w && i < 8; i++)
        d[i] = (u8)(v >> (i * 8));
    } else { /* vec → vec */
      const u8 *s = vm->V[src & 31];
      for (int i = 0; i < w; i++)
        d[i] = s[i];
    }
  }
  return 5;
}

/* MOVI Vt, #imm128 */
static inline u32 h_vmovi(vm_ctx_t *vm) {
  u8 vt = vm->bc[vm->pc + 1];
  u64 lo = rd64(&vm->bc[vm->pc + 2]);
  u64 hi = rd64(&vm->bc[vm->pc + 10]);
  u8 *d = vm->V[vt & 31];
  for (int i = 0; i < 8; i++)
    d[i] = (u8)(lo >> (i * 8));
  for (int i = 0; i < 8; i++)
    d[8 + i] = (u8)(hi >> (i * 8));
  return 18;
}

#endif /* H_VSIMD_H */
