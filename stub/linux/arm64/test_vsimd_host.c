/*
 * test_vsimd_host.c — SIMD&FP handler 宿主机单元测试 (可用 x86 gcc 直接编译运行)
 *
 * 直接调用 h_vsimd.h 的 handler, 用手工字节码验证 V 寄存器搬运语义,
 * 绕过 trampoline/注入/目标机运行 (后者在 qemu-user + 静态 nostdlib 下有
 * 与本改动无关的已知崩溃, 连整数访存也崩)。
 *
 *   gcc -I stub/linux/arm64 -o /tmp/tv stub/linux/arm64/test_vsimd_host.c && /tmp/tv
 */
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "vm_types.h"
#include "vm_decode.h"
#include "vm_handlers/h_vsimd.h"

static int fails = 0;
#define CHECK(c, msg)                                                          \
  do {                                                                         \
    if (!(c)) {                                                                \
      printf("FAIL: %s\n", msg);                                               \
      fails++;                                                                 \
    } else                                                                     \
      printf("ok  : %s\n", msg);                                              \
  } while (0)

static vm_ctx_t vm; /* static: 结构体很大 (含 16KB vm_stk) */

static void wr32(u8 *p, u32 v) { memcpy(p, &v, 4); }

int main(void) {
  u8 bc[64];
  vm.bc = bc;

  /* ---- VMOVI: V[3] ← 128-bit 立即数 ---- */
  {
    u64 lo = 0x1122334455667788ULL, hi = 0x99AABBCCDDEEFF00ULL;
    bc[0] = OP_VMOVI;
    bc[1] = 3;
    memcpy(&bc[2], &lo, 8);
    memcpy(&bc[10], &hi, 8);
    vm.pc = 0;
    u32 sz = h_vmovi(&vm);
    CHECK(sz == 18, "vmovi returns size 18");
    CHECK(memcmp(vm.V[3], &lo, 8) == 0 && memcmp(vm.V[3] + 8, &hi, 8) == 0,
          "vmovi writes full 128-bit value");
  }

  /* ---- VSTORE width=16 (STR q) ---- */
  {
    u8 dst[32];
    memset(dst, 0xEE, sizeof(dst));
    for (int i = 0; i < 16; i++)
      vm.V[5][i] = (u8)(i + 1);
    bc[0] = OP_VSTORE;
    bc[1] = 5;      /* vt */
    bc[2] = 0;      /* base = R[0] */
    wr32(&bc[3], 0); /* off = 0 */
    bc[7] = 16;     /* width */
    vm.R[0] = (u64)(uintptr_t)dst;
    vm.pc = 0;
    u32 sz = h_vstore(&vm);
    CHECK(sz == 8, "vstore returns size 8");
    CHECK(memcmp(dst, vm.V[5], 16) == 0, "vstore q writes 16 bytes");
    CHECK(dst[16] == 0xEE, "vstore q no overrun past 16");
  }

  /* ---- VSTORE width=8 (STR d) — 只写 8 字节 ---- */
  {
    u8 dst[16];
    memset(dst, 0xEE, sizeof(dst));
    for (int i = 0; i < 16; i++)
      vm.V[6][i] = (u8)(0xA0 + i);
    bc[0] = OP_VSTORE;
    bc[1] = 6;
    bc[2] = 0;
    wr32(&bc[3], 0);
    bc[7] = 8; /* width = 8 (D) */
    vm.R[0] = (u64)(uintptr_t)dst;
    vm.pc = 0;
    h_vstore(&vm);
    CHECK(memcmp(dst, vm.V[6], 8) == 0, "vstore d writes 8 bytes");
    CHECK(dst[8] == 0xEE, "vstore d leaves byte 8 untouched");
  }

  /* ---- VLOAD width=16: V[7] ← mem, 高位已被清零验证 (width=8) ---- */
  {
    u8 src[16];
    for (int i = 0; i < 16; i++)
      src[i] = (u8)(0x10 + i);
    /* 先给 V[7] 填满非零, 验证 load 会清高位 */
    memset(vm.V[7], 0xFF, 16);
    bc[0] = OP_VLOAD;
    bc[1] = 7;
    bc[2] = 0;
    wr32(&bc[3], 0);
    bc[7] = 8; /* width 8 → 低 8 字节来自 mem, 高 8 字节清零 */
    vm.R[0] = (u64)(uintptr_t)src;
    vm.pc = 0;
    u32 sz = h_vload(&vm);
    CHECK(sz == 8, "vload returns size 8");
    CHECK(memcmp(vm.V[7], src, 8) == 0, "vload d reads low 8 bytes");
    int hi_zero = 1;
    for (int i = 8; i < 16; i++)
      if (vm.V[7][i] != 0)
        hi_zero = 0;
    CHECK(hi_zero, "vload d zeroes high 8 bytes");
  }

  /* ---- 带偏移的 VLOAD/VSTORE ---- */
  {
    u8 buf[64];
    memset(buf, 0, sizeof(buf));
    for (int i = 0; i < 16; i++)
      vm.V[8][i] = (u8)(i ^ 0x5A);
    bc[0] = OP_VSTORE;
    bc[1] = 8;
    bc[2] = 0;
    wr32(&bc[3], 32); /* off = 32 */
    bc[7] = 16;
    vm.R[0] = (u64)(uintptr_t)buf;
    vm.pc = 0;
    h_vstore(&vm);
    CHECK(memcmp(buf + 32, vm.V[8], 16) == 0, "vstore with offset 32");
    /* 读回到 V[9] */
    bc[0] = OP_VLOAD;
    bc[1] = 9;
    vm.pc = 0;
    h_vload(&vm);
    CHECK(memcmp(vm.V[9], vm.V[8], 16) == 0, "vload with offset 32 round-trip");
  }

  /* ---- VSTOREP / VLOADP (STP/LDP q) ---- */
  {
    u8 buf[64];
    memset(buf, 0, sizeof(buf));
    for (int i = 0; i < 16; i++) {
      vm.V[10][i] = (u8)(0x11 + i);
      vm.V[11][i] = (u8)(0x80 + i);
    }
    bc[0] = OP_VSTOREP;
    bc[1] = 10; /* vt1 */
    bc[2] = 11; /* vt2 */
    bc[3] = 0;  /* base */
    wr32(&bc[4], 0);
    bc[8] = 16; /* width */
    vm.R[0] = (u64)(uintptr_t)buf;
    vm.pc = 0;
    u32 sz = h_vstorep(&vm);
    CHECK(sz == 9, "vstorep returns size 9");
    CHECK(memcmp(buf, vm.V[10], 16) == 0, "vstorep elem1 @ +0");
    CHECK(memcmp(buf + 16, vm.V[11], 16) == 0, "vstorep elem2 @ +16");
    /* 读回 */
    bc[0] = OP_VLOADP;
    bc[1] = 12;
    bc[2] = 13;
    vm.pc = 0;
    h_vloadp(&vm);
    CHECK(memcmp(vm.V[12], vm.V[10], 16) == 0 &&
              memcmp(vm.V[13], vm.V[11], 16) == 0,
          "vloadp round-trip both elements");
  }

  /* ---- SP 边界检查: base=31, 地址在 vm_stk 内 → 执行; 越界 → 跳过不崩 ---- */
  {
    for (int i = 0; i < 16; i++)
      vm.V[14][i] = (u8)(0xC0 + i);
    /* R[31] 指向 vm_stk 中部, 存 16 字节应成功 */
    u64 spaddr = (u64)(uintptr_t)&vm.vm_stk[100];
    vm.R[31] = spaddr;
    bc[0] = OP_VSTORE;
    bc[1] = 14;
    bc[2] = 31; /* base = SP */
    wr32(&bc[3], 0);
    bc[7] = 16;
    vm.pc = 0;
    h_vstore(&vm);
    CHECK(memcmp(&vm.vm_stk[100], vm.V[14], 16) == 0,
          "vstore via SP within vm_stk works");

    /* R[31] 指向无效区 (远离 vm_stk) → 应静默跳过, 不解引用 */
    vm.R[31] = 0x1000; /* 必然在 vm_stk 范围外 */
    bc[1] = 15;
    vm.pc = 0;
    u32 sz = h_vstore(&vm); /* 不应崩溃 */
    CHECK(sz == 8, "vstore via out-of-range SP is skipped (no crash)");
  }

  printf("\n%s (%d failures)\n", fails ? "FAILED" : "ALL PASS", fails);
  return fails ? 1 : 0;
}
