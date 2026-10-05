/*
 * test_vfp_host.c — 标量浮点 handler 宿主机单元测试 (x86 gcc 直接编译运行)
 *
 * IEEE-754 单/双精度基本运算在 x86 与 arm64 上按标准正确舍入, 结果一致,
 * 故宿主机单测可可靠验证 h_vfp.h 的语义。
 *
 *   gcc -I stub/linux/arm64 -lm -o /tmp/tvfp stub/linux/arm64/test_vfp_host.c && /tmp/tvfp
 */
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "vm_types.h"
#include "vm_decode.h"
#include "vm_handlers/h_vfp.h"

static int fails = 0;
#define CHECK(c, msg)                                                          \
  do {                                                                         \
    if (!(c)) {                                                                \
      printf("FAIL: %s\n", msg);                                               \
      fails++;                                                                 \
    } else                                                                     \
      printf("ok  : %s\n", msg);                                               \
  } while (0)

static vm_ctx_t vm;

int main(void) {
  u8 bc[32];
  vm.bc = bc;

  /* ---- FADD/FSUB/FMUL/FDIV (double) ---- */
  {
    vf_setd(&vm, 1, 1.5);
    vf_setd(&vm, 2, 2.25);
    u8 subs[4] = {0, 1, 2, 3};
    double exp[4] = {3.75, -0.75, 3.375, 1.5 / 2.25};
    const char *nm[4] = {"fadd d", "fsub d", "fmul d", "fdiv d"};
    for (int k = 0; k < 4; k++) {
      bc[0] = OP_VF_BIN;
      bc[1] = subs[k];
      bc[2] = 0;
      bc[3] = 1;
      bc[4] = 2;
      bc[5] = 8;
      vm.pc = 0;
      u32 sz = h_vfbin(&vm);
      if (k == 0)
        CHECK(sz == 6, "vfbin returns size 6");
      CHECK(vf_getd(&vm, 0) == exp[k], nm[k]);
    }
  }
  /* ---- FADD (float, 单精度路径) ---- */
  {
    vf_setf(&vm, 1, 1.5f);
    vf_setf(&vm, 2, 0.25f);
    bc[0] = OP_VF_BIN;
    bc[1] = 0;
    bc[2] = 0;
    bc[3] = 1;
    bc[4] = 2;
    bc[5] = 4;
    vm.pc = 0;
    h_vfbin(&vm);
    CHECK(vf_getf(&vm, 0) == 1.75f, "fadd s");
  }

  /* ---- FABS / FNEG / FSQRT ---- */
  {
    vf_setd(&vm, 1, -3.5);
    bc[0] = OP_VF_UN;
    bc[1] = 0;
    bc[2] = 0;
    bc[3] = 1;
    bc[4] = 8; /* FABS */
    vm.pc = 0;
    u32 sz = h_vfun(&vm);
    CHECK(sz == 5, "vfun returns size 5");
    CHECK(vf_getd(&vm, 0) == 3.5, "fabs d");
    bc[1] = 1; /* FNEG */
    vm.pc = 0;
    h_vfun(&vm);
    CHECK(vf_getd(&vm, 0) == 3.5, "fneg d (-3.5 → 3.5)");
    vf_setd(&vm, 1, 4.0);
    bc[1] = 2; /* FSQRT */
    vm.pc = 0;
    h_vfun(&vm);
    CHECK(vf_getd(&vm, 0) == 2.0, "fsqrt d (4→2)");
    vf_setf(&vm, 1, 9.0f);
    bc[4] = 4;
    vm.pc = 0;
    h_vfun(&vm);
    CHECK(vf_getf(&vm, 0) == 3.0f, "fsqrt s (9→3)");
  }

  /* ---- FCVT: f→f ---- */
  {
    vf_setf(&vm, 1, 1.25f);
    bc[0] = OP_VF_CVT;
    bc[1] = 0;
    bc[2] = 0;
    bc[3] = 1;
    bc[4] = 4;
    bc[5] = 8; /* S→D */
    vm.pc = 0;
    u32 sz = h_vfcvt(&vm);
    CHECK(sz == 6, "vfcvt returns size 6");
    CHECK(vf_getd(&vm, 0) == 1.25, "fcvt s→d");
    vf_setd(&vm, 1, 2.5);
    bc[4] = 8;
    bc[5] = 4; /* D→S */
    vm.pc = 0;
    h_vfcvt(&vm);
    CHECK(vf_getf(&vm, 0) == 2.5f, "fcvt d→s");
  }
  /* ---- FCVT: f→int (截断向零) ---- */
  {
    vf_setd(&vm, 1, 3.9);
    bc[0] = OP_VF_CVT;
    bc[1] = 1;
    bc[2] = 5;
    bc[3] = 1;
    bc[4] = 8;
    bc[5] = 8; /* FCVTZS x,d */
    vm.pc = 0;
    h_vfcvt(&vm);
    CHECK((i64)vm.R[5] == 3, "fcvtzs 3.9→3");
    vf_setd(&vm, 1, -3.9);
    vm.pc = 0;
    h_vfcvt(&vm);
    CHECK((i64)vm.R[5] == -3, "fcvtzs -3.9→-3 (trunc toward zero)");
  }
  /* ---- FCVT: int→f ---- */
  {
    vm.R[2] = (u64)(i64)-7;
    bc[0] = OP_VF_CVT;
    bc[1] = 3;
    bc[2] = 0;
    bc[3] = 2;
    bc[4] = 8;
    bc[5] = 8; /* SCVTF d,x */
    vm.pc = 0;
    h_vfcvt(&vm);
    CHECK(vf_getd(&vm, 0) == -7.0, "scvtf -7→-7.0");
    vm.R[2] = 0xFFFFFFFFULL; /* as unsigned 32 = 4294967295 */
    bc[1] = 4;
    bc[4] = 4;
    bc[5] = 8; /* UCVTF d,w */
    vm.pc = 0;
    h_vfcvt(&vm);
    CHECK(vf_getd(&vm, 0) == 4294967295.0, "ucvtf (u32)0xFFFFFFFF");
  }

  /* ---- FCMP → FL, 再用 vf_cond 验证常用条件 (有序) ---- */
  {
    /* 1.0 vs 2.0  (a<b) */
    vf_setd(&vm, 1, 1.0);
    vf_setd(&vm, 2, 2.0);
    bc[0] = OP_VF_CMP;
    bc[1] = 1;
    bc[2] = 2;
    bc[3] = 8;
    bc[4] = 0;
    vm.pc = 0;
    u32 sz = h_vfcmp(&vm);
    CHECK(sz == 5, "vfcmp returns size 5");
    CHECK(!vf_cond(vm.FL, 0x0), "1<2: EQ false");
    CHECK(vf_cond(vm.FL, 0x1), "1<2: NE true");
    CHECK(vf_cond(vm.FL, 0xB), "1<2: LT true");
    CHECK(!vf_cond(vm.FL, 0xC), "1<2: GT false");
    CHECK(vf_cond(vm.FL, 0xD), "1<2: LE true");
    CHECK(!vf_cond(vm.FL, 0xA), "1<2: GE false");
    /* 2.0 vs 2.0 (equal) */
    vf_setd(&vm, 1, 2.0);
    vm.pc = 0;
    h_vfcmp(&vm);
    CHECK(vf_cond(vm.FL, 0x0), "2==2: EQ true");
    CHECK(vf_cond(vm.FL, 0xA), "2==2: GE true");
    CHECK(vf_cond(vm.FL, 0xD), "2==2: LE true");
    CHECK(!vf_cond(vm.FL, 0xC), "2==2: GT false");
    /* 3.0 vs 2.0 (a>b) */
    vf_setd(&vm, 1, 3.0);
    vm.pc = 0;
    h_vfcmp(&vm);
    CHECK(vf_cond(vm.FL, 0xC), "3>2: GT true");
    CHECK(vf_cond(vm.FL, 0xA), "3>2: GE true");
    CHECK(!vf_cond(vm.FL, 0xB), "3>2: LT false");
    /* vs #0.0 */
    vf_setd(&vm, 1, -1.0);
    bc[4] = 1; /* isZero */
    vm.pc = 0;
    h_vfcmp(&vm);
    CHECK(vf_cond(vm.FL, 0xB), "-1 < 0: LT true");
  }

  /* ---- FCSEL: d = cond ? n : m ---- */
  {
    vf_setd(&vm, 1, 11.0);
    vf_setd(&vm, 2, 22.0);
    /* 置 FL 为 EQ (a==b) */
    vf_setd(&vm, 5, 1.0);
    vf_setd(&vm, 6, 1.0);
    bc[0] = OP_VF_CMP;
    bc[1] = 5;
    bc[2] = 6;
    bc[3] = 8;
    bc[4] = 0;
    vm.pc = 0;
    h_vfcmp(&vm);
    bc[0] = OP_VF_CSEL;
    bc[1] = 0;
    bc[2] = 1;
    bc[3] = 2;
    bc[4] = 0x0;
    bc[5] = 8; /* cond EQ → 选 n */
    vm.pc = 0;
    u32 sz = h_vfcsel(&vm);
    CHECK(sz == 6, "vfcsel returns size 6");
    CHECK(vf_getd(&vm, 0) == 11.0, "fcsel EQ true → n");
    bc[4] = 0x1; /* NE → 选 m */
    vm.pc = 0;
    h_vfcsel(&vm);
    CHECK(vf_getd(&vm, 0) == 22.0, "fcsel NE false → m");
  }

  printf("\n%s (%d failures)\n", fails ? "FAILED" : "ALL PASS", fails);
  return fails ? 1 : 0;
}
