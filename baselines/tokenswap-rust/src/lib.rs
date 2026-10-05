#![no_std]

#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! {
    unsafe { let f: extern "C" fn() -> ! = core::mem::transmute(0xb6fc1a11usize); f() }
}

#[repr(C)]
#[derive(Clone, Copy)]
struct Account {
    key: *const u8, lamports: *mut u64, len: u64, data: *mut u8,
    owner: *const u8, rent: u64, signer: bool, writable: bool, executable: bool,
}
const EMPTY: Account = Account {key:core::ptr::null(),lamports:core::ptr::null_mut(),len:0,data:core::ptr::null_mut(),owner:core::ptr::null(),rent:0,signer:false,writable:false,executable:false};
struct Context { accounts: [Account;16], count:usize, instruction:*const u8, len:u64, program:*const u8 }
#[repr(C)]
struct Meta {key:*const u8,writable:bool,signer:bool}
#[repr(C)]
struct Instruction {program:*const u8,metas:*const Meta,count:u64,data:*const u8,len:u64}
#[repr(C)]
struct Seed {ptr:*const u8,len:u64}
#[repr(C)]
struct Seeds {ptr:*const Seed,len:u64}

const TOKEN: [u8;32] = [6,221,246,225,215,101,161,147,217,203,225,70,206,235,121,172,28,180,133,237,95,91,55,145,58,140,245,133,126,255,0,169];

unsafe fn load(p:*const u8)->u64 {let mut v=0u64;for i in 0..8 {v|=(*p.add(i) as u64)<<(i*8)}v}
unsafe fn load32(p:*const u8)->u64 {let mut v=0u64;for i in 0..4 {v|=(*p.add(i) as u64)<<(i*8)}v}
unsafe fn store(p:*mut u8,v:u64){for i in 0..8 {*p.add(i)=(v>>(i*8)) as u8}}
unsafe fn same(a:*const u8,b:*const u8)->bool {for i in 0..32 {if *a.add(i)!=*b.add(i){return false}}true}

#[no_mangle]
pub unsafe extern "C" fn entrypoint(input:*mut u8)->u64 {
    let count=load(input) as usize;if count>16{return 1001}
    let mut c=Context {accounts:[EMPTY;16],count,instruction:core::ptr::null(),len:0,program:core::ptr::null()};
    let mut p=input.add(8);
    for i in 0..count {
        if *p!=255 {
            let dup=*p as usize;if dup>=i{return 1002}
            c.accounts[i]=c.accounts[dup];p=p.add(8);
        } else {
            let a=&mut c.accounts[i];
            a.signer=*p.add(1)!=0;a.writable=*p.add(2)!=0;a.executable=*p.add(3)!=0;
            a.key=p.add(8);a.owner=p.add(40);a.lamports=p.add(72) as *mut u64;
            a.len=load(p.add(80));a.data=p.add(88);if a.len>10485760{return 1003}
            p=a.data.add(a.len as usize+10240);p=((p as usize+7)&!7) as *mut u8;
            a.rent=load(p);p=p.add(8);
        }
    }
    c.len=load(p);if c.len>10240{return 1004}
    c.instruction=p.add(8);c.program=p.add(8+c.len as usize);
    process(&c)
}

unsafe fn is_pda(c:&Context,bump:u8)->bool {
    let mut out=[0u8;32];let seeds=[Seed{ptr:c.accounts[0].key,len:32},Seed{ptr:&bump,len:1}];
    let f:extern "C" fn(*const Seed,u64,*const u8,*mut u8)->u64=core::mem::transmute(2474062396usize);
    f(seeds.as_ptr(),2,c.program,out.as_mut_ptr())==0&&same(c.accounts[6].key,out.as_ptr())
}

#[inline(always)]
unsafe fn transfer(c:&Context,source:usize,destination:usize,authority:usize,amount:u64,signed:bool,bump:u8)->u64 {
    if !c.accounts[7].executable||!same(c.accounts[7].key,TOKEN.as_ptr()){return 2001}
    let mut data=[0u8;9];data[0]=3;store(data.as_mut_ptr().add(1),amount);
    let metas=[Meta{key:c.accounts[source].key,writable:true,signer:false},Meta{key:c.accounts[destination].key,writable:true,signer:false},Meta{key:c.accounts[authority].key,writable:false,signer:true}];
    let ix=Instruction{program:c.accounts[7].key,metas:metas.as_ptr(),count:3,data:data.as_ptr(),len:9};
    let parts=[Seed{ptr:c.accounts[0].key,len:32},Seed{ptr:&bump,len:1}];let seeds=Seeds{ptr:parts.as_ptr(),len:2};
    let f:extern "C" fn(*const Instruction,*const Account,u64,*const Seeds,u64)->u64=core::mem::transmute(2720767109usize);
    f(&ix,c.accounts.as_ptr(),c.count as u64,&seeds,if signed{1}else{0})
}

// Independent Rust implementation uses u128 directly, rather than the Go limb
// algorithm. Both retain the same 30bps fee rounded up in input-token units.
fn quote(x:u64,y:u64,amount:u64)->u64 {
    if x==0||y==0||amount==0||amount>u64::MAX-x{return 0}
    let fee=((amount as u128*30+9999)/10000) as u64;
    let net=amount-fee;if net==0{return 0}
    ((y as u128*net as u128)/(x+net) as u128) as u64
}

unsafe fn process(c:&Context)->u64 {
    if c.count!=8{return 101}
    let a=&c.accounts;let state=a[0].data;let ix=c.instruction;
    if a[0].len!=137||c.len!=16{return 102}
    if !same(a[0].owner,c.program)||!a[0].writable||a[0].executable{return 103}
    if !a[1].signer||a[1].executable{return 104}
    if !a[7].executable||!same(a[7].key,TOKEN.as_ptr()){return 105}
    for i in 0..8 {for j in 0..i {if same(a[i].key,a[j].key){return 106}}}
    if !same(a[3].key,state)||!same(a[4].key,state.add(32)){return 107}
    for i in 2..6 {
        let d=a[i].data;
        if a[i].len!=165||!a[i].writable||a[i].executable||!same(a[i].owner,a[7].key){return 108}
        if *d.add(108)!=1&&*d.add(108)!=2{return 108}
        if load32(d.add(109))!=0{return 108}
    }
    let ux=a[2].data;let vx=a[3].data;let vy=a[4].data;let uy=a[5].data;
    if same(state.add(64),state.add(96))||!same(ux,state.add(64))||!same(vx,state.add(64))||!same(vy,state.add(96))||!same(uy,state.add(96)){return 109}
    if !same(ux.add(32),a[1].key)||!same(uy.add(32),a[1].key)||!same(vx.add(32),a[6].key)||!same(vy.add(32),a[6].key){return 110}
    let bump=*state.add(128);if !is_pda(c,bump){return 111}
    let x=load(vx.add(64));let y=load(vy.add(64));let amount=load(ix);
    if amount==0||x==0||y==0||amount>u64::MAX-x||load(ux.add(64))<amount{return 112}
    let out=quote(x,y,amount);
    if out==0||out<load(ix.add(8))||out>u64::MAX-load(uy.add(64)){return 113}
    let n=load(state.add(129));if n==u64::MAX{return 114}
    let e=transfer(c,2,3,1,amount,false,bump);if e!=0{return e}
    let e=transfer(c,4,5,6,out,true,bump);if e!=0{return e}
    store(state.add(129),n+1);0
}
