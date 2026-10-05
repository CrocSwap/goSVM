#![allow(unexpected_cfgs)]
use borsh::{BorshDeserialize,BorshSerialize};
use solana_program::{account_info::AccountInfo,entrypoint::ProgramResult,program_error::ProgramError,pubkey::Pubkey,hash::hash};
mod kernels;
solana_program::entrypoint!(process_instruction);

#[derive(BorshSerialize,BorshDeserialize)]
pub struct Command { pub tag:u64,pub nonce:u64,pub cursor:u64,pub count:u64,pub seed:u64,pub digest:[u8;32] }
fn get(s:&[u8],o:usize)->u64 {u64::from_le_bytes(s[o..o+8].try_into().unwrap())}
fn put(s:&mut [u8],o:usize,v:u64){s[o..o+8].copy_from_slice(&v.to_le_bytes())}
fn fail(n:u32)->ProgramResult{Err(ProgramError::Custom(n))}
pub fn process_instruction(id:&Pubkey,a:&[AccountInfo],ix:&[u8])->ProgramResult {
    if a.len()!=4{return fail(101)}
    for i in 0..4{for j in 0..i{if a[i].key==a[j].key{return fail(105)}}}
    let mut s=a[0].try_borrow_mut_data()?;let cfg=a[2].try_borrow_data()?;let data=a[3].try_borrow_data()?;
    if s.len()!=256||cfg.len()!=64||data.len()!=2048||ix.len()!=72{return fail(102)}
    if !a[0].is_writable||a[0].executable||a[2].executable||a[3].executable||a[0].owner!=id||a[2].owner!=id||a[3].owner!=id{return fail(103)}
    if !a[1].is_signer||a[1].executable||s[..32]!=a[1].key.to_bytes()||cfg[..32]!=a[1].key.to_bytes(){return fail(104)}
    if get(&cfg,32)==0||get(&cfg,32)>16||get(&cfg,40)!=256||get(&cfg,48)>1000{return fail(106)}
    let cmd=Command::try_from_slice(ix).map_err(|_|ProgramError::Custom(102))?;
    if cmd.nonce!=get(&s,72){return fail(201)}
    let phase=get(&s,64);let calls=get(&s,192);if calls==u64::MAX{return fail(202)}
    match cmd.tag {
        0=>{
            if phase!=0{return fail(203)}
            let h=hash(&data).to_bytes();s[128..160].copy_from_slice(&h);
            if h!=cmd.digest{return fail(204)}
            s[32..64].copy_from_slice(&h);put(&mut s,64,1);put(&mut s,80,0);put(&mut s,88,cmd.seed);
        },
        1=>{
            if phase!=1{return fail(203)}
            if cmd.cursor!=get(&s,80)||cmd.cursor>256||cmd.count==0||cmd.count>get(&cfg,32)||cmd.count>256-cmd.cursor{return fail(205)}
            let fee=cmd.count*get(&cfg,48);let credits=get(&s,200);if credits<fee{return fail(206)}
            let h=hash(&data).to_bytes();s[128..160].copy_from_slice(&h);
            if h!=s[32..64]{return fail(204)}
            let mut acc=get(&s,88);
            for i in cmd.cursor..cmd.cursor+cmd.count {let word=get(&data,i as usize*8);acc=kernels::dispatch(word>>58,acc^word)}
            put(&mut s,80,cmd.cursor+cmd.count);put(&mut s,88,acc);put(&mut s,200,credits-fee);
            if cmd.cursor+cmd.count==256{put(&mut s,64,2)}
        },
        2|3=>{
            if (cmd.tag==2&&phase!=1&&phase!=2)||(cmd.tag==3&&phase!=2){return fail(203)}
            let h=hash(&s[64..96]).to_bytes();s[128..160].copy_from_slice(&h);
            if h!=cmd.digest{return fail(207)}
            s[96..128].copy_from_slice(&h);if cmd.tag==3{put(&mut s,64,3)}
        },
        4=>{
            if phase!=3{return fail(203)}
            if cmd.nonce==u64::MAX{return fail(208)}
            put(&mut s,64,0);put(&mut s,72,cmd.nonce+1);put(&mut s,80,0);put(&mut s,88,0);
            s[32..64].fill(0);s[96..128].fill(0);
        },
        _=>return fail(209),
    }
    put(&mut s,192,calls+1);Ok(())
}
