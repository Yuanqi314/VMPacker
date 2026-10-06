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

  /* ---- DUP 元素: v0.4s 各 lane = v1.s[2] ---- */
  {
    for (int i = 0; i < 4; i++) setlane(1, i * 4, 4, 11 * (i + 1)); /* 11,22,33,44 */
    bc[0] = OP_V_DUP_E; bc[1] = 0; bc[2] = 1; bc[3] = 4; bc[4] = 2; bc[5] = 16;
    vm.pc = 0;
    u32 sz = h_vdupe(&vm);
    CHECK(sz == 6, "vdupe size 6");
    CHECK(getlane(0, 0, 4) == 33 && getlane(0, 4, 4) == 33 &&
          getlane(0, 8, 4) == 33 && getlane(0, 12, 4) == 33, "dup v0.4s, v1.s[2]");
    /* Q=0 (.2s) 清零高 8 字节 */
    bc[5] = 8; vm.pc = 0; h_vdupe(&vm);
    int hz = 1; for (int i = 8; i < 16; i++) if (vm.V[0][i]) hz = 0;
    CHECK(hz, "dup Q=0 zeroes upper 8 bytes");
  }
  /* ---- DUP 通用: v0.4s 各 lane = R[1]; es 截断 ---- */
  {
    vm.R[1] = 0x1122334455667788ULL;
    bc[0] = OP_V_DUP_G; bc[1] = 0; bc[2] = 1; bc[3] = 4; bc[4] = 16;
    vm.pc = 0;
    u32 sz = h_vdupg(&vm);
    CHECK(sz == 5, "vdupg size 5");
    CHECK(getlane(0, 0, 4) == 0x55667788 && getlane(0, 12, 4) == 0x55667788,
          "dup v0.4s, w1 (低 32 位复制)");
    vm.R[1] = 0x1FF;
    bc[3] = 1; bc[4] = 8; vm.pc = 0; h_vdupg(&vm);
    CHECK(getlane(0, 0, 1) == 0xFF && getlane(0, 7, 1) == 0xFF, "dup .8b 截断到字节");
  }
  /* ---- UMOV: 零扩展 ---- */
  {
    setlane(1, 8, 4, 0xDEADBEEF); /* v1.s[2] */
    bc[0] = OP_V_MOV2R; bc[1] = 0; bc[2] = 1; bc[3] = 4; bc[4] = 2; bc[5] = 0; bc[6] = 0;
    vm.pc = 0;
    u32 sz = h_vmov2r(&vm);
    CHECK(sz == 7, "vmov2r size 7");
    CHECK(vm.R[0] == 0xDEADBEEFULL, "umov w0, v1.s[2]");
    setlane(1, 8, 8, 0x1122334455667788ULL); /* v1.d[1] */
    bc[3] = 8; bc[4] = 1; bc[6] = 1; vm.pc = 0; h_vmov2r(&vm);
    CHECK(vm.R[0] == 0x1122334455667788ULL, "umov x0, v1.d[1]");
  }
  /* ---- SMOV: 符号扩展 ---- */
  {
    setlane(1, 0, 1, 0xFF); /* v1.b[0] = -1 */
    bc[0] = OP_V_MOV2R; bc[1] = 0; bc[2] = 1; bc[3] = 1; bc[4] = 0; bc[5] = 1; bc[6] = 0;
    vm.pc = 0; h_vmov2r(&vm);
    CHECK(vm.R[0] == 0xFFFFFFFFULL, "smov w0, v1.b[0] (符号扩展到 32, 高 32 清零)");
    bc[6] = 1; vm.pc = 0; h_vmov2r(&vm); /* smov x0 */
    CHECK(vm.R[0] == 0xFFFFFFFFFFFFFFFFULL, "smov x0, v1.b[0] (符号扩展到 64)");
    setlane(1, 0, 1, 0x7F); bc[6] = 1; vm.pc = 0; h_vmov2r(&vm);
    CHECK(vm.R[0] == 0x7FULL, "smov x0, v1.b[0]=0x7F (正值)");
  }
  /* ---- INS 通用: 仅改目标 lane, 保留其余 ---- */
  {
    for (int i = 0; i < 4; i++) setlane(0, i * 4, 4, 0xA0A0A000 + i);
    vm.R[1] = 0xCAFEBABE;
    bc[0] = OP_V_INS_G; bc[1] = 0; bc[2] = 1; bc[3] = 4; bc[4] = 1; /* v0.s[1] */
    vm.pc = 0;
    u32 sz = h_vinsg(&vm);
    CHECK(sz == 5, "vinsg size 5");
    CHECK(getlane(0, 4, 4) == 0xCAFEBABE, "ins v0.s[1], w1");
    CHECK(getlane(0, 0, 4) == 0xA0A0A000 && getlane(0, 8, 4) == 0xA0A0A002 &&
          getlane(0, 12, 4) == 0xA0A0A003, "ins 保留其它 lane");
  }
  /* ---- INS 元素: v0.s[1] = v1.s[2] ---- */
  {
    for (int i = 0; i < 4; i++) setlane(0, i * 4, 4, 0xB0B0B000 + i);
    setlane(1, 8, 4, 0x12345678); /* v1.s[2] */
    bc[0] = OP_V_INS_E; bc[1] = 0; bc[2] = 1; bc[3] = 4; bc[4] = 1; bc[5] = 2;
    vm.pc = 0;
    u32 sz = h_vinse(&vm);
    CHECK(sz == 6, "vinse size 6");
    CHECK(getlane(0, 4, 4) == 0x12345678, "ins v0.s[1], v1.s[2]");
    CHECK(getlane(0, 0, 4) == 0xB0B0B000 && getlane(0, 12, 4) == 0xB0B0B003,
          "ins 元素保留其它 lane");
  }

  printf("\n%s (%d failures)\n", fails ? "FAILED" : "ALL PASS", fails);
  return fails ? 1 : 0;
}
