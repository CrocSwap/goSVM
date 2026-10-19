package compiler

import (
	"os/exec"
	"testing"
)

// TestFormerEscapesMoveToHeap runs the programs that were rejected for an
// escaping reference before allocation existed (the pointer and view
// lifetime tables): each now compiles, moves the escaping local to the heap
// and returns the value native Go would.
func TestFormerEscapesMoveToHeap(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		source string
		want   string
	}{
		"loop back edge":          {`func bad(in *uint64)*uint64{var n uint64;p:=in;q:=in;for j:=uint64(0);j<3;j++{q=p;p=&n};return q};func Process(s,i []byte)uint64{var n uint64=9;return *bad(&n)}`, "0"},
		"local return":            {`func bad()*uint64{var n uint64=5;return &n};func Process(s,i []byte)uint64{return *bad()}`, "5"},
		"parameter copy":          {`func bad(n uint64)*uint64{return &n};func Process(s,i []byte)uint64{p:=bad(1);q:=bad(2);return *p*10+*q}`, "12"},
		"literal return":          {`type V struct{N uint64};func bad()*V{return &V{N:1}};func Process(s,i []byte)uint64{a,b:=bad(),bad();a.N=3;return a.N*10+b.N}`, "31"},
		"forwarded local":         {`func keep(p *uint64)*uint64{return p};func bad()*uint64{var n uint64=6;return keep(&n)};func Process(s,i []byte)uint64{return *bad()}`, "6"},
		"tuple local":             {`func keep(p *uint64)(uint64,*uint64){return 0,p};func bad()(uint64,*uint64){var n uint64=7;return keep(&n)};func Process(s,i []byte)uint64{_,p:=bad();return *p}`, "7"},
		"block escape":            {`func Process(s,i []byte)uint64{var p *uint64;{var n uint64=8;p=&n};return *p}`, "8"},
		"loop escape":             {`func Process(s,i []byte)uint64{var p,q *uint64;for j:=uint64(0);j<2;j++ {var n uint64=j+1;q=p;p=&n};return *p*10+*q}`, "21"},
		"conditional escape":      {`func Process(s,i []byte)uint64{var p *uint64;if len(i)>0{n:=uint64(i[0]);p=&n};return *p}`, "7"},
		"block tuple":             {`func keep(p *uint64)(*uint64,uint64){return p,1};func Process(s,i []byte)uint64{var p *uint64;var n uint64;{local:=uint64(1);p,n=keep(&local)};return *p+n}`, "2"},
		"receiver copy":           {`type V struct{N uint64};func(v V)Bad()*uint64{return &v.N};func Process(s,i []byte)uint64{v:=V{N:4};p:=v.Bad();*p=9;return v.N*10+*p}`, "49"},
		"callee local method":     {`type V struct{N uint64};func(v *V)Field()*uint64{return &v.N};func bad()*uint64{v:=V{N:2};return v.Field()};func Process(s,i []byte)uint64{return *bad()}`, "2"},
		"method expression local": {`type V struct{N uint64};func(v *V)Field()*uint64{return &v.N};func bad()*uint64{v:=V{N:3};return (*V).Field(&v)};func Process(s,i []byte)uint64{return *bad()}`, "3"},
		"pointer fields":          {`type V struct{P *uint64};func Process(s,i []byte)uint64{n:=uint64(4);v:=V{P:&n};*v.P=5;return n}`, "5"},
		"local array view":        {`func bad()[]byte{var a [32]byte;a[1]=3;return a[:]};func Process(s,i []byte)uint64{return uint64(len(bad()))+uint64(bad()[1])}`, "35"},
		"value parameter view":    {`func bad(a [32]byte)[]byte{return a[:]};func Process(s,i []byte)uint64{var a [32]byte;v:=bad(a);v[0]=1;return uint64(len(v))+uint64(a[0])}`, "32"},
		"value receiver view":     {`type K [32]byte;func(k K)Bad()[]byte{return k[:]};func Process(s,i []byte)uint64{var k K;return uint64(len(k.Bad()))}`, "32"},
		"block view":              {`func Process(s,i []byte)uint64{var v []byte;{var k [32]byte;k[2]=1;v=k[:]};return uint64(len(v))+uint64(v[2])}`, "33"},
		"loop view":               {`func Process(s,i []byte)uint64{var v,w []byte;for j:=uint64(0);j<2;j++{var k [32]byte;k[0]=byte(j);w=v;v=k[:]};return uint64(v[0])*10+uint64(w[0])}`, "10"},
		"converted local":         {`func bad()[]byte{var k [32]byte;v:=k[:];return []byte(v)};func Process(s,i []byte)uint64{return uint64(len(bad()))}`, "32"},
		"forwarded tuple":         {`func keep(v []byte)(uint64,[]byte){return 0,v};func bad()(uint64,[]byte){var k [32]byte;return keep(k[:])};func Process(s,i []byte)uint64{_,v:=bad();return uint64(len(v))}`, "32"},
		"view element pointer":    {`func bad()*byte{var k [32]byte;k[0]=6;v:=k[:];return &v[0]};func Process(s,i []byte)uint64{return uint64(*bad())}`, "6"},
		"word views":              {`func Process(s,i []byte)uint64{var k [4]uint64;v:=k[:];v[3]=8;return v[3]+k[3]}`, "16"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := Compile("moved.go", []byte("package p;"+c.source))
			if err != nil {
				t.Fatal(err)
			}
			bin := hostCompile(t, out, DefaultHeapBytes, `
#include <stdio.h>
#include <stdlib.h>
void abort(void) {puts("abort"); exit(0);}
int main(void){u8 s[24]={0},i[16]={7};printf("%llu",go_Process(GOSVM_SLICE(s,24),GOSVM_SLICE(i,16)));return 0;}`, false)
			got, err := exec.Command(bin).CombinedOutput()
			if err != nil || string(got) != c.want {
				t.Fatalf("got %q (%v), want %s", got, err, c.want)
			}
		})
	}
}
