
#include <stdio.h>
#include <stdlib.h>
#include <setjmp.h>
typedef unsigned char u8;typedef unsigned int u32;typedef unsigned long long u64;
typedef long long i64;typedef _Bool boolean;typedef struct {u8 *ptr;i64 len,cap;} slice;
static jmp_buf jump;static u8 data[80];static int panic;static u64 result;
void abort(void){panic=1;longjmp(jump,1);}
static u8 *at(slice s,u64 i){if(i>=(u64)s.len)abort();return s.ptr+i;}
static u64 v222(slice v220,u64 v221){
u64 v2513 = 0ULL;
{
u64 v2514 = 0ULL;
for (;;) {
u64 t2515 = v2514;
boolean t2516 = ((u64)t2515 < (u64)8ULL);
if (!(t2516)) break;
{
u64 t2517 = v2513;
slice t2518 = v220;
u64 t2519 = v221;
u64 t2520 = v2514;
u64 t2521 = ((u64)t2519 + (u64)t2520);
u8 t2522 = *at(t2518,t2521);
u64 t2523 = (u64)t2522;
u64 t2524 = v2514;
u64 t2525 = ((u64)8ULL * (u64)t2524);
u64 t2526 = t2525 >= 64 ? 0 : ((u64)t2523 << t2525);
u64 t2527 = ((u64)t2517 | (u64)t2526);
v2513 = t2527;
}
v2514++;
}
}
u64 t2528 = v2513;
return t2528;
}
static u32 v225(slice v223,u64 v224){
u32 v2529 = 0ULL;
{
u64 v2530 = 0ULL;
for (;;) {
u64 t2531 = v2530;
boolean t2532 = ((u64)t2531 < (u64)4ULL);
if (!(t2532)) break;
{
u32 t2533 = v2529;
slice t2534 = v223;
u64 t2535 = v224;
u64 t2536 = v2530;
u64 t2537 = ((u64)t2535 + (u64)t2536);
u8 t2538 = *at(t2534,t2537);
u32 t2539 = (u32)t2538;
u64 t2540 = v2530;
u64 t2541 = ((u64)8ULL * (u64)t2540);
u32 t2542 = t2541 >= 32 ? 0 : ((u64)t2539 << t2541);
u32 t2543 = ((u64)t2533 | (u64)t2542);
v2529 = t2543;
}
v2530++;
}
}
u32 t2544 = v2529;
return t2544;
}
static void v238(slice v235,u64 v236,u64 v237){
#if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
if (v236<=(u64)v235.len && 8ULL<=(u64)v235.len-v236) { __builtin_memcpy(v235.ptr+v236,&v237,8); return; }
#endif

{
u64 v2575 = 0ULL;
for (;;) {
u64 t2576 = v2575;
boolean t2577 = ((u64)t2576 < (u64)8ULL);
if (!(t2577)) break;
{
slice t2578 = v235;
u64 t2579 = v236;
u64 t2580 = v2575;
u64 t2581 = ((u64)t2579 + (u64)t2580);
u64 t2582 = v237;
u64 t2583 = v2575;
u64 t2584 = ((u64)8ULL * (u64)t2583);
u64 t2585 = t2584 >= 64 ? 0 : ((u64)t2582 >> t2584);
u8 t2586 = (u8)t2585;
*at(t2578,t2581) = t2586;
}
v2575++;
}
}
}
static void v242(slice v239,u64 v240,u32 v241){
#if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
if (v240<=(u64)v239.len && 4ULL<=(u64)v239.len-v240) { __builtin_memcpy(v239.ptr+v240,&v241,4); return; }
#endif

{
u64 v2587 = 0ULL;
for (;;) {
u64 t2588 = v2587;
boolean t2589 = ((u64)t2588 < (u64)4ULL);
if (!(t2589)) break;
{
slice t2590 = v239;
u64 t2591 = v240;
u64 t2592 = v2587;
u64 t2593 = ((u64)t2591 + (u64)t2592);
u32 t2594 = v241;
u64 t2595 = v2587;
u64 t2596 = ((u64)8ULL * (u64)t2595);
u32 t2597 = t2596 >= 32 ? 0 : ((u64)t2594 >> t2596);
u8 t2598 = (u8)t2597;
*at(t2590,t2593) = t2598;
}
v2587++;
}
}
}
int main(void){unsigned op,alignment;u64 length,offset,value;char hex[161];
while(scanf("%u %u %llu %llu %llu %160s",&op,&alignment,&length,&offset,&value,hex)==6){
for(int j=0;j<80;j++){unsigned x;sscanf(hex+2*j,"%2x",&x);data[j]=(u8)x;}
panic=0;result=0;
if(!setjmp(jump)){slice b={data+alignment,(i64)length,(i64)length};
switch(op){case 0:result=v222(b,offset);break;case 1:result=v225(b,offset);break;case 2:v238(b,offset,value);break;case 3:v242(b,offset,(u32)value);break;}}
printf("%c:%llu:",panic?'P':op<2?'R':'W',panic?0:result);
for(int j=0;j<80;j++)printf("%02x",data[j]);puts("");}return 0;}
