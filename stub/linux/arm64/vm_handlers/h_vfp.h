/*
 * h_vfp.h — 标量浮点运算 handler (S=单精度 / D=双精度)
 *
 * 值存于 V 寄存器低位 (S 低 4 字节, D 低 8 字节, 小端)。S 运算用 float、
 * D 运算用 double, 不经 double 中转以避免二次舍入。
 *   OP_VF_BIN  [subop][d][n][m][w]   FADD/FSUB/FMUL/FDIV
 *   OP_VF_UN   [subop][d][n][w]      FABS/FNEG/FSQRT
 *   OP_VF_CVT  [kind][d][n][inw][outw]  单双互转 / 浮点↔整数
 *   OP_VF_CMP  [n][m][w][isZero]     置 FL
 *   OP_VF_CSEL [d][n][m][cond][w]    d = cond?n:m
 *
 * 比较标志约定 (3-flag 模型, 有序比较完全正确):
 *   FL_ZERO=(a==b)  FL_SIGN=(a<b)  FL_CARRY=(a<b)
 * 无序 (NaN 操作数) 下 b.gt/b.ge 等可能与硬件不一致 (VM 无 V 标志), 属已知限制。
 */
#ifndef H_VFP_H
#define H_VFP_H

#include "../vm_decode.h"
#include "../vm_types.h"

/* ---- V 低位 ↔ float/double 读写 (union 做类型双关, 不依赖 memcpy) ---- */
static inline float vf_getf(vm_ctx_t *vm, u8 r) {
  union {
    u32 u;
    float f;
  } x;
  const u8 *p = vm->V[r & 31];
  x.u = (u32)p[0] | ((u32)p[1] << 8) | ((u32)p[2] << 16) | ((u32)p[3] << 24);
  return x.f;
}
static inline double vf_getd(vm_ctx_t *vm, u8 r) {
  union {
    u64 u;
    double d;
  } x;
  const u8 *p = vm->V[r & 31];
  x.u = 0;
  for (int i = 0; i < 8; i++)
    x.u |= (u64)p[i] << (i * 8);
  return x.d;
}
static inline void vf_setf(vm_ctx_t *vm, u8 r, float f) {
  union {
    u32 u;
    float f;
  } x;
  x.f = f;
  u8 *p = vm->V[r & 31];
  for (int i = 0; i < 16; i++)
    p[i] = 0;
  for (int i = 0; i < 4; i++)
    p[i] = (u8)(x.u >> (i * 8));
}
static inline void vf_setd(vm_ctx_t *vm, u8 r, double d) {
  union {
    u64 u;
    double d;
  } x;
  x.d = d;
  u8 *p = vm->V[r & 31];
  for (int i = 0; i < 16; i++)
    p[i] = 0;
  for (int i = 0; i < 8; i++)
    p[i] = (u8)(x.u >> (i * 8));
}

/* fsqrt: arm64 用硬件指令, 宿主机测试 (x86) 用 __builtin_sqrt */
static inline double vf_sqrtd(double a) {
#if defined(__aarch64__)
  double r;
  __asm__("fsqrt %d0, %d1" : "=w"(r) : "w"(a));
  return r;
#else
  return __builtin_sqrt(a);
#endif
}
static inline float vf_sqrtf(float a) {
#if defined(__aarch64__)
  float r;
  __asm__("fsqrt %s0, %s1" : "=w"(r) : "w"(a));
  return r;
#else
  return __builtin_sqrtf(a);
#endif
}

/* ---- FP 二元: [op][subop][d][n][m][w] ---- */
static inline u32 h_vfbin(vm_ctx_t *vm) {
  u8 sub = vm->bc[vm->pc + 1], d = vm->bc[vm->pc + 2];
  u8 n = vm->bc[vm->pc + 3], m = vm->bc[vm->pc + 4], w = vm->bc[vm->pc + 5];
  if (w == 8) {
    double a = vf_getd(vm, n), b = vf_getd(vm, m), r = 0;
    switch (sub) {
    case 0: r = a + b; break;
    case 1: r = a - b; break;
    case 2: r = a * b; break;
    case 3: r = a / b; break;
    }
    vf_setd(vm, d, r);
  } else {
    float a = vf_getf(vm, n), b = vf_getf(vm, m), r = 0;
    switch (sub) {
    case 0: r = a + b; break;
    case 1: r = a - b; break;
    case 2: r = a * b; break;
    case 3: r = a / b; break;
    }
    vf_setf(vm, d, r);
  }
  return 6;
}

/* ---- FP 一元: [op][subop][d][n][w]  (0 abs, 1 neg, 2 sqrt) ----
 * FSQRT 走硬件/内建; FABS/FNEG 用符号位操作 (正确处理 -0.0 / NaN)。 */
static inline u32 h_vfun(vm_ctx_t *vm) {
  u8 sub = vm->bc[vm->pc + 1], d = vm->bc[vm->pc + 2];
  u8 n = vm->bc[vm->pc + 3], w = vm->bc[vm->pc + 4];
  if (sub == 2) { /* FSQRT */
    if (w == 8)
      vf_setd(vm, d, vf_sqrtd(vf_getd(vm, n)));
    else
      vf_setf(vm, d, vf_sqrtf(vf_getf(vm, n)));
    return 5;
  }
  /* FABS(clear sign) / FNEG(flip sign), 直接位操作 */
  const u8 *p = vm->V[n & 31];
  u8 *o = vm->V[d & 31];
  if (w == 8) {
    u64 u = 0;
    for (int i = 0; i < 8; i++)
      u |= (u64)p[i] << (i * 8);
    u = (sub == 0) ? (u & ~((u64)1 << 63)) : (u ^ ((u64)1 << 63));
    for (int i = 0; i < 16; i++)
      o[i] = 0;
    for (int i = 0; i < 8; i++)
      o[i] = (u8)(u >> (i * 8));
  } else {
    u32 u = (u32)p[0] | ((u32)p[1] << 8) | ((u32)p[2] << 16) | ((u32)p[3] << 24);
    u = (sub == 0) ? (u & ~((u32)1 << 31)) : (u ^ ((u32)1 << 31));
    for (int i = 0; i < 16; i++)
      o[i] = 0;
    for (int i = 0; i < 4; i++)
      o[i] = (u8)(u >> (i * 8));
  }
  return 5;
}

/* ---- 转换: [op][kind][d][n][inw][outw] ----
 * kind: 0 f→f, 1 f→s(signed int), 2 f→u(unsigned int), 3 s→f, 4 u→f */
static inline u32 h_vfcvt(vm_ctx_t *vm) {
  u8 kind = vm->bc[vm->pc + 1], d = vm->bc[vm->pc + 2];
  u8 n = vm->bc[vm->pc + 3], inw = vm->bc[vm->pc + 4], outw = vm->bc[vm->pc + 5];
  switch (kind) {
  case 0: { /* f→f */
    double v = (inw == 8) ? vf_getd(vm, n) : (double)vf_getf(vm, n);
    if (outw == 8)
      vf_setd(vm, d, v);
    else
      vf_setf(vm, d, (float)v);
    break;
  }
  case 1: { /* f→signed int (截断向零) */
    double v = (inw == 8) ? vf_getd(vm, n) : (double)vf_getf(vm, n);
    i64 r = (i64)v;
    if (outw == 4)
      r = (i64)(i32)r;
    vm->R[d & 31] = (u64)r;
    break;
  }
  case 2: { /* f→unsigned int (截断向零) */
    double v = (inw == 8) ? vf_getd(vm, n) : (double)vf_getf(vm, n);
    u64 r = (u64)v;
    if (outw == 4)
      r = (u32)r;
    vm->R[d & 31] = r;
    break;
  }
  case 3: { /* signed int→f */
    i64 iv = (i64)vm->R[n & 31];
    if (inw == 4)
      iv = (i64)(i32)iv;
    if (outw == 8)
      vf_setd(vm, d, (double)iv);
    else
      vf_setf(vm, d, (float)iv);
    break;
  }
  case 4: { /* unsigned int→f */
    u64 uv = vm->R[n & 31];
    if (inw == 4)
      uv = (u32)uv;
    if (outw == 8)
      vf_setd(vm, d, (double)uv);
    else
      vf_setf(vm, d, (float)uv);
    break;
  }
  }
  return 6;
}

/* ---- FCMP: [op][n][m][w][isZero] → 置 FL ---- */
static inline u32 h_vfcmp(vm_ctx_t *vm) {
  u8 n = vm->bc[vm->pc + 1], m = vm->bc[vm->pc + 2];
  u8 w = vm->bc[vm->pc + 3], isZero = vm->bc[vm->pc + 4];
  int eq, lt;
  if (w == 8) {
    double a = vf_getd(vm, n), b = isZero ? 0.0 : vf_getd(vm, m);
    eq = (a == b);
    lt = (a < b);
  } else {
    float a = vf_getf(vm, n), b = isZero ? 0.0f : vf_getf(vm, m);
    eq = (a == b);
    lt = (a < b);
  }
  vm->FL = 0;
  if (eq)
    vm->FL |= FL_ZERO;
  if (lt)
    vm->FL |= FL_SIGN | FL_CARRY;
  return 5;
}

/* 条件码对 FL 的求值 (与整数分支/CSEL 语义一致) */
static inline int vf_cond(u32 FL, u8 cond) {
  int Z = (FL & FL_ZERO) != 0, N = (FL & FL_SIGN) != 0, C = (FL & FL_CARRY) != 0;
  switch (cond & 0xF) {
  case 0x0: return Z;             /* EQ */
  case 0x1: return !Z;            /* NE */
  case 0x2: return !C;            /* CS/HS */
  case 0x3: return C;             /* CC/LO */
  case 0x4: return N;             /* MI */
  case 0x5: return !N;            /* PL */
  case 0x6: return 0;             /* VS (无 V) */
  case 0x7: return 1;             /* VC */
  case 0x8: return !C && !Z;      /* HI */
  case 0x9: return C || Z;        /* LS */
  case 0xA: return !N;            /* GE */
  case 0xB: return N;             /* LT */
  case 0xC: return !Z && !N;      /* GT */
  case 0xD: return Z || N;        /* LE */
  default: return 1;              /* AL/NV */
  }
}

/* ---- FCSEL: [op][d][n][m][cond][w] → d = cond?n:m ---- */
static inline u32 h_vfcsel(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  u8 m = vm->bc[vm->pc + 3], cond = vm->bc[vm->pc + 4], w = vm->bc[vm->pc + 5];
  u8 src = vf_cond(vm->FL, cond) ? n : m;
  /* 整段 128 位拷贝 (标量只需低 w 字节, 但拷全更稳妥) */
  const u8 *s = vm->V[src & 31];
  u8 *dst = vm->V[d & 31];
  (void)w;
  for (int i = 0; i < 16; i++)
    dst[i] = s[i];
  return 6;
}

#endif /* H_VFP_H */
