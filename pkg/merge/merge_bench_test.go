package merge

import (
	"fmt"
	"testing"
)

func BenchmarkMyersDiff_100Lines(b *testing.B) {
	var a, c []string
	for i := 0; i < 100; i++ {
		a = append(a, fmt.Sprintf("line number %d in file", i))
		if i%10 == 0 {
			c = append(c, fmt.Sprintf("modified line number %d", i))
		} else {
			c = append(c, fmt.Sprintf("line number %d in file", i))
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MyersDiff(a, c)
	}
}

func BenchmarkMyersDiff_1000Lines(b *testing.B) {
	var a, c []string
	for i := 0; i < 1000; i++ {
		a = append(a, fmt.Sprintf("function Foo_%d() { return %d }", i, i))
		if i%20 == 0 {
			c = append(c, fmt.Sprintf("function Foo_%d_Optimized() { return %d }", i, i*2))
		} else {
			c = append(c, fmt.Sprintf("function Foo_%d() { return %d }", i, i))
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MyersDiff(a, c)
	}
}

func BenchmarkDiff3Merge_CleanIndependent(b *testing.B) {
	var base, ours, theirs []string
	for i := 0; i < 200; i++ {
		base = append(base, fmt.Sprintf("const ConfigVal%d = %d;", i, i))
		if i == 20 {
			ours = append(ours, "const ConfigVal20 = 9999; // ours update")
		} else {
			ours = append(ours, fmt.Sprintf("const ConfigVal%d = %d;", i, i))
		}

		if i == 180 {
			theirs = append(theirs, "const ConfigVal180 = 8888; // theirs update")
		} else {
			theirs = append(theirs, fmt.Sprintf("const ConfigVal%d = %d;", i, i))
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, clean := Diff3Merge(base, ours, theirs)
		if !clean {
			b.Fatalf("expected clean merge")
		}
	}
}
