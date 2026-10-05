/*
 * demo_insn_simdfp.c — SIMD&FP 数据搬运指令 VMP 测试
 *
 * vtest() 在 -O2 下会被编译器向量化为 movi / ldp·stp q / ldr·str q·d 等
 * "只搬数据" 的 SIMD&FP 指令 (无向量/浮点运算), 正是 JNI_OnLoad 那类函数的
 * 典型形态。保护前后行为应完全一致。
 *
 * 编译: aarch64-linux-gnu-gcc -O2 -nostdlib -march=armv8-a \
 *         -o demo_simdfp demo_insn_simdfp.c
 * 保护: vmpacker -func vtest -o demo_simdfp.vmp demo_simdfp
 * 期望: 保护前后退出码一致 = (sum of input) & 0xFF
 */

typedef struct {
  unsigned long a[6]; /* 48 字节 */
} Blob;

/* 全 SIMD 搬运: 结构体拷贝 (ldr/str q 或 ldp/stp q) + memset 置零 (movi+stp q)
 * + 8 字节 double 槽读写 (ldr/str d)。只有搬运, 没有浮点/向量运算。 */
__attribute__((noinline)) unsigned long vtest(const Blob *in) {
  Blob tmp = *in; /* 结构体拷贝 */
  unsigned char zbuf[48];
  for (int i = 0; i < 48; i++)
    zbuf[i] = 0; /* 置零 → movi + stp q */

  /* 把 tmp 的字节搬进 zbuf (再拷贝一次), 保证 zbuf 的写被使用 */
  const unsigned char *src = (const unsigned char *)tmp.a;
  for (int i = 0; i < 48; i++)
    zbuf[i] = src[i];

  unsigned long s = 0;
  for (int i = 0; i < 48; i++)
    s += zbuf[i];
  return s;
}

void _start(void) {
  static const Blob in = {{0x0102030405060708UL, 0x1112131415161718UL,
                           0x2122232425262728UL, 0x3132333435363738UL,
                           0x4142434445464748UL, 0x5152535455565758UL}};
  unsigned long r = vtest(&in);

  register long x0 asm("x0") = (long)(r & 0xFF);
  register long x8 asm("x8") = 93; /* __NR_exit */
  asm volatile("svc #0" : : "r"(x0), "r"(x8));
}
