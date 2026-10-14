package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// goSizeClasses is Go 1.22's runtime class_to_size (runtime/sizeclasses.go).
var goSizeClasses = []uint64{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048, 2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384, 18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768}

// modelCap is the capacity the emitted gosvm_grow gives append(s, num
// elements) when len(s)+num exceeds cap(s): a transcription of Go 1.22's
// growslice (nextslicecap, then roundupsize with malloc headers).
func modelCap(oldLen, oldCap, num, size uint64, scan bool) uint64 {
	newLen := oldLen + num
	newCap := oldCap
	if newLen > 2*oldCap {
		newCap = newLen
	} else if oldCap < 256 {
		newCap = 2 * oldCap
	} else {
		for newCap < newLen {
			newCap += (newCap + 768) >> 2
		}
	}
	mem := newCap * size
	req := mem
	rounded := uint64(0)
	if req <= 32768-8 {
		if scan && req > 512 {
			req += 8
		}
		for _, class := range goSizeClasses {
			if class >= req {
				rounded = class - (req - mem)
				break
			}
		}
	} else {
		rounded = (req + 8191) &^ 8191
	}
	return rounded / size
}

// growthTypes are element types of many sizes, with and without pointers.
func growthTypes() []reflect.Type {
	var out []reflect.Type
	pointer := reflect.TypeOf((*uint64)(nil))
	for size := 1; size <= 1100; size++ {
		if size <= 160 || size%8 == 0 && size <= 1100 || size%37 == 0 {
			out = append(out, reflect.ArrayOf(size, reflect.TypeOf(byte(0))))
		}
		if size%8 == 0 && (size <= 1100) && (size <= 200 || size%24 == 0 || size >= 504 && size <= 536) {
			fields := []reflect.StructField{{Name: "P", Type: pointer}}
			if size > 8 {
				fields = append(fields, reflect.StructField{Name: "A", Type: reflect.ArrayOf(size-8, reflect.TypeOf(byte(0)))})
			}
			out = append(out, reflect.StructOf(fields))
		}
	}
	return out
}

func hasPointers(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		return true
	case reflect.Array:
		return t.Len() > 0 && hasPointers(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if hasPointers(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

// TestGrowthModelMatchesGoRuntime checks the model against Go's own
// growslice through reflect.AppendSlice (reflect.Value.grow calls
// runtime.growslice with the same new length as append), for every type in
// growthTypes and append sequences of 1, 2, 3, 7 and 100 elements, until the
// backing array exceeds 300 KiB.
func TestGrowthModelMatchesGoRuntime(t *testing.T) {
	if v := strings.TrimPrefix(runtime.Version(), "go"); !strings.HasPrefix(v, "1.22") {
		t.Skipf("the oracle is Go 1.22's runtime; running %s", v)
	}
	checked := 0
	for _, typ := range growthTypes() {
		size, scan := uint64(typ.Size()), hasPointers(typ)
		for _, step := range []int{1, 2, 3, 7, 100} {
			s := reflect.MakeSlice(reflect.SliceOf(typ), 0, 0)
			extra := reflect.MakeSlice(reflect.SliceOf(typ), step, step)
			for uint64(s.Cap())*size < 300*1024 {
				oldLen, oldCap := uint64(s.Len()), uint64(s.Cap())
				next := reflect.AppendSlice(s, extra)
				want := uint64(next.Cap())
				if oldLen+uint64(step) <= oldCap {
					if want != oldCap {
						t.Fatalf("%v: in-place append changed cap %d -> %d", typ, oldCap, want)
					}
				} else if got := modelCap(oldLen, oldCap, uint64(step), size, scan); got != want {
					t.Fatalf("%v (size %d, pointers %v): append %d to len %d cap %d: model cap %d, Go %d", typ, size, scan, step, oldLen, oldCap, got, want)
				}
				if oldLen+uint64(step) > oldCap {
					checked++
				}
				s = next
			}
		}
	}
	if checked < 20000 {
		t.Fatalf("only %d growth steps checked", checked)
	}
	t.Logf("%d growth steps across %d element types match Go's runtime", checked, len(growthTypes()))
}

// TestGrowthRuntimeMatchesModel runs the emitted C helpers (gosvm_grow) on
// the host over a grid of lengths, capacities, counts, sizes and both
// pointer modes, and compares every resulting capacity with the model.
func TestGrowthRuntimeMatchesModel(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang is needed")
	}
	type row struct{ oldLen, oldCap, num, size, scan uint64 }
	var rows []row
	sizes := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 15, 16, 17, 24, 31, 32, 40, 48, 56, 64, 72, 80, 96, 100, 128, 129, 200, 256, 384, 500, 504, 505, 511, 512, 513, 520, 640, 1000, 1024, 1032, 2048, 4000, 4096, 5000, 8192, 10000, 32760, 32768, 40000}
	caps := []uint64{0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 100, 255, 256, 257, 300, 511, 512, 513, 1000, 1024, 3000, 5000, 20000, 100000}
	for _, size := range sizes {
		for _, oldCap := range caps {
			for _, num := range []uint64{1, 2, 3, 5, 100, 257, 1000} {
				for _, oldLen := range []uint64{oldCap, oldCap / 2} {
					if oldLen+num <= oldCap {
						continue
					}
					next := modelCap(oldLen, oldCap, num, size, true)
					if next*size > 60<<20 || oldLen*size > 1<<20 {
						continue
					}
					for scan := uint64(0); scan < 2; scan++ {
						rows = append(rows, row{oldLen, oldCap, num, size, scan})
					}
				}
			}
		}
	}
	var input strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&input, "%d %d %d %d %d\n", r.oldLen, r.oldCap, r.num, r.size, r.scan)
	}
	// A 64 MiB host heap stands in for the SBF heap region; the budget check
	// is exercised separately. Each row starts from a fresh heap.
	source := "typedef unsigned char u8;typedef unsigned int u32;typedef unsigned long long u64;typedef long long i64;typedef _Bool boolean;\n" +
		"typedef struct { u8 *ptr; i64 len,cap; } slice;\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n" +
		"static u64 host_heap[(64<<20)/8+16];\n#define GOSVM_HEAP_BASE ((u8 *)host_heap)\n" +
		strings.Replace(heapRuntimeFor(MaxHeapBytes), "#define GOSVM_HEAP_BYTES 262144ULL", "#define GOSVM_HEAP_BYTES (64ULL<<20)", 1) + `
static u8 old[1<<20];
int main(void){u64 l,c,n,s,scan;
while(scanf("%llu %llu %llu %llu %llu",&l,&c,&n,&s,&scan)==5){
 memset(host_heap,0,64);
 slice v={old,(i64)l,(i64)c}; /* only the first len elements are read */
 slice r=gosvm_grow(v,n,s,scan);
 printf("%lld %lld\n",r.len,r.cap);
}return 0;}
`
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "grow.c"), filepath.Join(dir, "grow")
	if err = os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-Werror", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("%d results for %d rows", len(lines), len(rows))
	}
	for j, r := range rows {
		want := fmt.Sprintf("%d %d", r.oldLen+r.num, modelCap(r.oldLen, r.oldCap, r.num, r.size, r.scan == 1))
		if lines[j] != want {
			t.Fatalf("%+v: C %q, model %q", r, lines[j], want)
		}
	}
	t.Logf("%d C growth results match the model", len(rows))
}

// TestGrowthModelMatchesCompiledAppend checks the model against append on
// concrete compiled types (no reflect), which is what native fixtures run.
func TestGrowthModelMatchesCompiledAppend(t *testing.T) {
	type big struct {
		P *uint64
		A [64]uint64
	}
	check := func(name string, size uintptr, scan bool, appendOne func() (uint64, uint64, uint64)) {
		for {
			oldLen, oldCap, newCap := appendOne()
			if oldLen+1 > oldCap {
				if want := modelCap(oldLen, oldCap, 1, uint64(size), scan); newCap != want {
					t.Fatalf("%s: len %d cap %d: Go cap %d, model %d", name, oldLen, oldCap, newCap, want)
				}
			}
			if newCap*uint64(size) > 200*1024 {
				return
			}
		}
	}
	var b []byte
	check("byte", 1, false, func() (uint64, uint64, uint64) {
		l, c := len(b), cap(b)
		b = append(b, 1)
		return uint64(l), uint64(c), uint64(cap(b))
	})
	var w [][3]uint64
	check("[3]uint64", unsafe.Sizeof([3]uint64{}), false, func() (uint64, uint64, uint64) {
		l, c := len(w), cap(w)
		w = append(w, [3]uint64{})
		return uint64(l), uint64(c), uint64(cap(w))
	})
	var p []*uint64
	check("*uint64", 8, true, func() (uint64, uint64, uint64) {
		l, c := len(p), cap(p)
		p = append(p, nil)
		return uint64(l), uint64(c), uint64(cap(p))
	})
	var s [][]byte
	check("[]byte", 24, true, func() (uint64, uint64, uint64) {
		l, c := len(s), cap(s)
		s = append(s, nil)
		return uint64(l), uint64(c), uint64(cap(s))
	})
	var g []big
	check("big", unsafe.Sizeof(big{}), true, func() (uint64, uint64, uint64) {
		l, c := len(g), cap(g)
		g = append(g, big{})
		return uint64(l), uint64(c), uint64(cap(g))
	})
}
