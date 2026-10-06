/*
 * h_vneon.h — 基础 NEON 向量运算 handler
 *
 * 对 V 寄存器按 lane 并行:
 *   OP_VEC_BIN  [subop][d][n][m][esize][nbytes]  整数 ADD/SUB/MUL (逐 lane)
 *   OP_VEC_LOGIC[subop][d][n][m][nbytes]         AND/BIC/ORR/ORN/EOR (逐字节)
 *   OP_VEC_NOT  [d][n][nbytes]                   NOT (逐字节)
 *   OP_VEC_FBIN [subop][d][n][m][esize][nbytes]  浮点 FADD/FSUB/FMUL/FDIV (逐 lane)
 *
 * nbytes=8 (Q=0) 时写回后清零 V 寄存器高 64 位 (符合 AArch64 语义)。
 * 用临时缓冲避免 d==n / d==m 的别名问题。
 */
#ifndef H_VNEON_H
#define H_VNEON_H

#include "../vm_decode.h"
#include "../vm_types.h"

static inline u64 vn_rdlane(const u8 *p, int off, int es) {
  u64 v = 0;
  for (int i = 0; i < es; i++)
    v |= (u64)p[off + i] << (i * 8);
  return v;
}
static inline void vn_wrlane(u8 *p, int off, int es, u64 v) {
  for (int i = 0; i < es; i++)
    p[off + i] = (u8)(v >> (i * 8));
}
static inline float vn_rdf(const u8 *p, int off) {
  union {
    u32 u;
    float f;
  } x;
  x.u = (u32)vn_rdlane(p, off, 4);
  return x.f;
}
static inline double vn_rdd(const u8 *p, int off) {
  union {
    u64 u;
    double d;
  } x;
  x.u = vn_rdlane(p, off, 8);
  return x.d;
}
static inline void vn_wrf(u8 *p, int off, float f) {
  union {
    u32 u;
    float f;
  } x;
  x.f = f;
  vn_wrlane(p, off, 4, x.u);
}
static inline void vn_wrd(u8 *p, int off, double d) {
  union {
    u64 u;
    double d;
  } x;
  x.d = d;
  vn_wrlane(p, off, 8, x.u);
}

/* 写回 tmp[0..nb) 到 V[d], 并清零高位 (nb..16) */
static inline void vn_commit(vm_ctx_t *vm, u8 d, const u8 *tmp, int nb) {
  u8 *pd = vm->V[d & 31];
  for (int i = 0; i < nb; i++)
    pd[i] = tmp[i];
  for (int i = nb; i < 16; i++)
    pd[i] = 0;
}

/* 整数向量二元: [op][subop][d][n][m][esize][nbytes] */
static inline u32 h_vecbin(vm_ctx_t *vm) {
  u8 sub = vm->bc[vm->pc + 1], d = vm->bc[vm->pc + 2], n = vm->bc[vm->pc + 3];
  u8 m = vm->bc[vm->pc + 4], es = vm->bc[vm->pc + 5], nb = vm->bc[vm->pc + 6];
  const u8 *pn = vm->V[n & 31], *pm = vm->V[m & 31];
  u8 tmp[16];
  u64 mask = (es >= 8) ? ~(u64)0 : (((u64)1 << (es * 8)) - 1);
  for (int off = 0; off + es <= nb; off += es) {
    u64 a = vn_rdlane(pn, off, es), b = vn_rdlane(pm, off, es), r = 0;
    switch (sub) {
    case 0: r = a + b; break;
    case 1: r = a - b; break;
    case 2: r = a * b; break;
    }
    vn_wrlane(tmp, off, es, r & mask);
  }
  vn_commit(vm, d, tmp, nb);
  return 7;
}

/* 浮点向量二元: [op][subop][d][n][m][esize][nbytes] (esize 4=S / 8=D) */
static inline u32 h_vecfbin(vm_ctx_t *vm) {
  u8 sub = vm->bc[vm->pc + 1], d = vm->bc[vm->pc + 2], n = vm->bc[vm->pc + 3];
  u8 m = vm->bc[vm->pc + 4], es = vm->bc[vm->pc + 5], nb = vm->bc[vm->pc + 6];
  const u8 *pn = vm->V[n & 31], *pm = vm->V[m & 31];
  u8 tmp[16];
  for (int off = 0; off + es <= nb; off += es) {
    if (es == 8) {
      double a = vn_rdd(pn, off), b = vn_rdd(pm, off), r = 0;
      switch (sub) {
      case 0: r = a + b; break;
      case 1: r = a - b; break;
      case 2: r = a * b; break;
      case 3: r = a / b; break;
      }
      vn_wrd(tmp, off, r);
    } else {
      float a = vn_rdf(pn, off), b = vn_rdf(pm, off), r = 0;
      switch (sub) {
      case 0: r = a + b; break;
      case 1: r = a - b; break;
      case 2: r = a * b; break;
      case 3: r = a / b; break;
      }
      vn_wrf(tmp, off, r);
    }
  }
  vn_commit(vm, d, tmp, nb);
  return 7;
}

/* 位运算: [op][subop][d][n][m][nbytes]  0 AND 1 BIC 2 ORR 3 ORN 4 EOR */
static inline u32 h_veclogic(vm_ctx_t *vm) {
  u8 sub = vm->bc[vm->pc + 1], d = vm->bc[vm->pc + 2], n = vm->bc[vm->pc + 3];
  u8 m = vm->bc[vm->pc + 4], nb = vm->bc[vm->pc + 5];
  const u8 *pn = vm->V[n & 31], *pm = vm->V[m & 31];
  u8 tmp[16];
  for (int i = 0; i < nb; i++) {
    u8 a = pn[i], b = pm[i], r = 0;
    switch (sub) {
    case 0: r = a & b; break;
    case 1: r = a & (u8)~b; break; /* BIC */
    case 2: r = a | b; break;
    case 3: r = a | (u8)~b; break; /* ORN */
    case 4: r = a ^ b; break;
    }
    tmp[i] = r;
  }
  vn_commit(vm, d, tmp, nb);
  return 6;
}

/* NOT: [op][d][n][nbytes] */
static inline u32 h_vecnot(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2], nb = vm->bc[vm->pc + 3];
  const u8 *pn = vm->V[n & 31];
  u8 tmp[16];
  for (int i = 0; i < nb; i++)
    tmp[i] = (u8)~pn[i];
  vn_commit(vm, d, tmp, nb);
  return 4;
}

/* ---- NEON lane 搬运 ---- */

/* DUP 元素: [op][d][n][es][index][nbytes]  Vd 各 lane = Vn[index] */
static inline u32 h_vdupe(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2], es = vm->bc[vm->pc + 3];
  u8 idx = vm->bc[vm->pc + 4], nb = vm->bc[vm->pc + 5];
  u64 val = vn_rdlane(vm->V[n & 31], idx * es, es);
  u8 tmp[16];
  for (int off = 0; off + es <= nb; off += es)
    vn_wrlane(tmp, off, es, val);
  vn_commit(vm, d, tmp, nb);
  return 6;
}

/* DUP 通用: [op][d][rn][es][nbytes]  Vd 各 lane = R[rn] (截断到 es) */
static inline u32 h_vdupg(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], rn = vm->bc[vm->pc + 2];
  u8 es = vm->bc[vm->pc + 3], nb = vm->bc[vm->pc + 4];
  u64 mask = (es >= 8) ? ~(u64)0 : (((u64)1 << (es * 8)) - 1);
  u64 val = vm->R[rn & 31] & mask;
  u8 tmp[16];
  for (int off = 0; off + es <= nb; off += es)
    vn_wrlane(tmp, off, es, val);
  vn_commit(vm, d, tmp, nb);
  return 5;
}

/* UMOV/SMOV: [op][d][n][es][index][sign][sf]  R[d] = 扩展(Vn[index])
 *   sign 0 零扩展(UMOV) 1 符号扩展(SMOV); sf=0 时结果截断到 32 位 (写 W) */
static inline u32 h_vmov2r(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2], es = vm->bc[vm->pc + 3];
  u8 idx = vm->bc[vm->pc + 4], sign = vm->bc[vm->pc + 5], sf = vm->bc[vm->pc + 6];
  u64 raw = vn_rdlane(vm->V[n & 31], idx * es, es);
  u64 out;
  if (sign) {
    int bits = es * 8;
    if (bits >= 64) {
      out = raw;
    } else {
      u64 m = (u64)1 << (bits - 1);
      out = (u64)(((i64)(raw ^ m)) - (i64)m); /* 符号扩展 es*8 → 64 */
    }
    if (!sf)
      out &= 0xFFFFFFFFULL; /* 写 W 清零高 32 */
  } else {
    out = raw; /* UMOV 零扩展 */
  }
  vm->R[d & 31] = out;
  return 7;
}

/* INS 通用: [op][d][rn][es][index]  Vd[index] = R[rn] (仅改该 lane) */
static inline u32 h_vinsg(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], rn = vm->bc[vm->pc + 2];
  u8 es = vm->bc[vm->pc + 3], idx = vm->bc[vm->pc + 4];
  vn_wrlane(vm->V[d & 31], idx * es, es, vm->R[rn & 31]);
  return 5;
}

/* INS 元素: [op][d][n][es][didx][sidx]  Vd[didx] = Vn[sidx] (仅改该 lane) */
static inline u32 h_vinse(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2], es = vm->bc[vm->pc + 3];
  u8 didx = vm->bc[vm->pc + 4], sidx = vm->bc[vm->pc + 5];
  u64 val = vn_rdlane(vm->V[n & 31], sidx * es, es); /* 先读, 避免 d==n 别名 */
  vn_wrlane(vm->V[d & 31], didx * es, es, val);
  return 6;
}

#endif /* H_VNEON_H */
