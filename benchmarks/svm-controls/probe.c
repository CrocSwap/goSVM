// Runtime-only SBFv3 probe; this does not extend the Go compiler/SDK surface.
typedef unsigned char u8;
typedef unsigned long long u64;
typedef long long i64;
typedef struct { u64 slot; i64 epoch_start_timestamp; u64 epoch;
                 u64 leader_schedule_epoch; i64 unix_timestamp; } Clock;
typedef struct { u64 lamports_per_byte_year; double exemption_threshold; u8 burn_percent; } Rent;
_Static_assert(sizeof(Clock)==40, "clock ABI");
_Static_assert(sizeof(Rent)==24, "rent ABI");
// Solana syscall names hashed with the SBF murmur3 convention.
#define get_clock ((u64 (*)(Clock *))0xd56b5fe9ULL)
#define get_rent ((u64 (*)(Rent *))0xbf7188f6ULL)
static u64 read64(const u8 *p) { u64 v=0; for(u64 i=0;i<8;i++) v|=(u64)p[i]<<(i*8); return v; }
u64 entrypoint(u8 *input) {
    if(read64(input)!=1 || input[8]!=255 || input[10]!=1) return 1;
    u64 n=read64(input+88); if(n!=72) return 2;
    Clock clock; Rent rent;
    if(get_clock(&clock) || get_rent(&rent)) return 3;
    u8 *data=input+96;
    u64 counter=read64(data+64)+1;
    const u8 *c=(const u8 *)&clock;
    const u8 *r=(const u8 *)&rent;
    for(u64 i=0;i<40;i++) data[i]=c[i];
    for(u64 i=0;i<16;i++) data[40+i]=r[i];
    for(u64 i=0;i<8;i++) data[56+i]=i==0?rent.burn_percent:0;
    for(u64 i=0;i<8;i++) data[64+i]=(u8)(counter>>(i*8));
    u8 *ix=(u8 *)(((u64)(data+n+10240)+7)&~7ULL)+8;
    if(read64(ix)!=1) return 4;
    return ix[8]==1?9:0; // mutate then fail to verify VM rollback
}
