/*
 * test_vneon_host.c — 基础 NEON 向量 handler 宿主机单元测试 (x86 gcc 直接编译运行)
 *   gcc -I stub/linux/arm64 -o /tmp/tvn stub/linux/arm64/test_vneon_host.c && /tmp/tvn
 */
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "vm_types.h"
#include "vm_decode.h"
#include "vm_handlers/h_vneon.h"

static int fails = 0;
#define CHECK(c, msg)                                                          \
  do {                                                                         \
    if (!(c)) { printf("FAIL: %s\n", msg); fails++; }                          \
    else printf("ok  : %s\n", msg);                                            \
  } while (0)

static vm_ctx_t vm;

/* 写一个 lane */
static void setlane(u8 r, int off, int es, u64 v) { vn_wrlane(vm.V[r & 31], off, es, v); }
static u64 getlane(u8 r, int off, int es) { return vn_rdlane(vm.V[r & 31], off, es); }

int main(void) {
  u8 bc[16];
  vm.bc = bc;

  /* ---- VADD v.4s (4×u32) ---- */
  {
    u32 a[4] = {1, 2, 3, 0xFFFFFFFF}, b[4] = {10, 20, 30, 1};
    for (int i = 0; i < 4; i++) { setlane(1, i * 4, 4, a[i]); setlane(2, i * 4, 4, b[i]); }
    bc[0] = OP_VEC_BIN; bc[1] = 0; bc[2] = 0; bc[3] = 1; bc[4] = 2; bc[5] = 4; bc[6] = 16;
    vm.pc = 0;
    u32 sz = h_vecbin(&vm);
    CHECK(sz == 7, "vecbin size 7");
    CHECK(getlane(0, 0, 4) == 11 && getlane(0, 4, 4) == 22 && getlane(0, 8, 4) == 33, "vadd 4s lanes");
    CHECK(getlane(0, 12, 4) == 0, "vadd 4s lane wrap (0xFFFFFFFF+1=0)");
  }
  /* ---- VADD v.8b (lane wrap at 8 bits) ---- */
  {
    setlane(1, 0, 1, 200); setlane(2, 0, 1, 100);
    bc[0] = OP_VEC_BIN; bc[1] = 0; bc[2] = 0; bc[3] = 1; bc[4] = 2; bc[5] = 1; bc[6] = 8;
    vm.pc = 0;
    h_vecbin(&vm);
    CHECK(getlane(0, 0, 1) == (u8)(200 + 100), "vadd 8b byte wrap (300&0xFF=44)");
    int hz = 1; for (int i = 8; i < 16; i++) if (vm.V[0][i]) hz = 0;
    CHECK(hz, "vadd Q=0 zeroes upper 8 bytes");
  }
  /* ---- VSUB / VMUL v.4s ---- */
  {
    setlane(1, 0, 4, 100); setlane(2, 0, 4, 30);
    bc[0] = OP_VEC_BIN; bc[1] = 1; bc[5] = 4; bc[6] = 16; vm.pc = 0; h_vecbin(&vm);
    CHECK(getlane(0, 0, 4) == 70, "vsub 4s");
    setlane(1, 0, 4, 7); setlane(2, 0, 4, 6);
    bc[1] = 2; vm.pc = 0; h_vecbin(&vm);
    CHECK(getlane(0, 0, 4) == 42, "vmul 4s");
  }
  /* ---- 逻辑 AND/ORR/EOR/BIC/ORN (byte-wise) ---- */
  {
    for (int i = 0; i < 16; i++) { vm.V[1][i] = 0xF0; vm.V[2][i] = 0x3C; }
    bc[0] = OP_VEC_LOGIC; bc[2] = 0; bc[3] = 1; bc[4] = 2; bc[5] = 16;
    bc[1] = 0; vm.pc = 0; h_veclogic(&vm); CHECK(vm.V[0][0] == (0xF0 & 0x3C), "vand");
    bc[1] = 2; vm.pc = 0; h_veclogic(&vm); CHECK(vm.V[0][0] == (0xF0 | 0x3C), "vorr");
    bc[1] = 4; vm.pc = 0; h_veclogic(&vm); CHECK(vm.V[0][0] == (0xF0 ^ 0x3C), "veor");
    bc[1] = 1; vm.pc = 0; h_veclogic(&vm); CHECK(vm.V[0][0] == (0xF0 & (u8)~0x3C), "vbic");
    bc[1] = 3; vm.pc = 0; h_veclogic(&vm); CHECK(vm.V[0][0] == (u8)(0xF0 | (u8)~0x3C), "vorn");
  }
  /* ---- NOT ---- */
  {
    for (int i = 0; i < 16; i++) vm.V[1][i] = 0xAA;
    bc[0] = OP_VEC_NOT; bc[1] = 0; bc[2] = 1; bc[3] = 16; vm.pc = 0;
    u32 sz = h_vecnot(&vm);
    CHECK(sz == 4 && vm.V[0][0] == 0x55, "vnot");
  }
  /* ---- VFADD v.2d / v.4s ---- */
  {
    vn_wrd(vm.V[1], 0, 1.5); vn_wrd(vm.V[1], 8, 2.5);
    vn_wrd(vm.V[2], 0, 0.25); vn_wrd(vm.V[2], 8, 0.5);
    bc[0] = OP_VEC_FBIN; bc[1] = 0; bc[2] = 0; bc[3] = 1; bc[4] = 2; bc[5] = 8; bc[6] = 16;
    vm.pc = 0;
    u32 sz = h_vecfbin(&vm);
    CHECK(sz == 7, "vecfbin size 7");
    CHECK(vn_rdd(vm.V[0], 0) == 1.75 && vn_rdd(vm.V[0], 8) == 3.0, "vfadd 2d lanes");
    vn_wrf(vm.V[1], 0, 1.5f); vn_wrf(vm.V[1], 4, 3.0f);
    vn_wrf(vm.V[2], 0, 0.5f); vn_wrf(vm.V[2], 4, 1.0f);
    bc[1] = 2; bc[5] = 4; bc[6] = 16; vm.pc = 0; h_vecfbin(&vm); /* fmul */
    CHECK(vn_rdf(vm.V[0], 0) == 0.75f && vn_rdf(vm.V[0], 4) == 3.0f, "vfmul 4s lanes");
  }

  printf("\n%s (%d failures)\n", fails ? "FAILED" : "ALL PASS", fails);
  return fails ? 1 : 0;
}
