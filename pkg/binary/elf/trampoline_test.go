package elf

import (
	"encoding/binary"
	"testing"

	"github.com/vmpacker/pkg/arch/arm64"
)

// BuildBranchTrampoline 应生成单条 4 字节 B 指令，且反汇编回原始目标地址。
func TestBuildBranchTrampoline(t *testing.T) {
	d := arm64.NewDecoder()
	cases := []struct {
		name              string
		funcAddr, thunkVA uint64
	}{
		{"forward", 0x1000, 0x40000},   // 向前跳
		{"backward", 0x400000, 0x1000}, // 向后跳
		{"adjacent", 0x2000, 0x2004},   // 相邻 (+4)
	}
	for _, c := range cases {
		b, err := BuildBranchTrampoline(c.funcAddr, c.thunkVA)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if len(b) != BranchTrampolineSize {
			t.Fatalf("%s: size = %d, want %d", c.name, len(b), BranchTrampolineSize)
		}
		raw := binary.LittleEndian.Uint32(b)
		// B = 0b000101 imm26 (opcode 字段应为 0x14000000 的高 6 位)
		if raw&0xFC000000 != 0x14000000 {
			t.Fatalf("%s: not a B instruction: 0x%08X", c.name, raw)
		}
		// 反汇编验证分支目标 = thunkVA。B 的立即数相对 B 指令自身地址。
		in := d.Decode(raw, 0)
		target := int64(c.funcAddr) + in.Imm
		if uint64(target) != c.thunkVA {
			t.Fatalf("%s: branch target = 0x%X, want 0x%X (imm=%d)",
				c.name, uint64(target), c.thunkVA, in.Imm)
		}
	}
}

// 超出 ±128MiB 或未对齐应报错。
func TestBuildBranchTrampoline_Errors(t *testing.T) {
	if _, err := BuildBranchTrampoline(0, 1<<28); err == nil {
		t.Fatal("expected out-of-range error for +256MiB target")
	}
	if _, err := BuildBranchTrampoline(1<<28, 0); err == nil {
		t.Fatal("expected out-of-range error for -256MiB target")
	}
	if _, err := BuildBranchTrampoline(0x1000, 0x1002); err == nil {
		t.Fatal("expected alignment error for non-4B-aligned target")
	}
	// 恰好 ±128MiB 边界: +128MiB 非法 (>=2^27), -128MiB 合法。
	if _, err := BuildBranchTrampoline(0, 1<<27); err == nil {
		t.Fatal("expected out-of-range error at +128MiB (exclusive)")
	}
	if _, err := BuildBranchTrampoline(1<<27, 0); err != nil {
		t.Fatalf("-128MiB should be in range, got %v", err)
	}
}
