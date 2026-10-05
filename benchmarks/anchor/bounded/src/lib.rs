use anchor_lang::prelude::*;
declare_id!("2HoXWo3sW3WZPbPvMStZJyuwQEruUsuruVJMb94BAbBN");
anchor_lang::solana_program::entrypoint!(checked_entry);
fn checked_entry<'info>(program_id:&Pubkey,accounts:&'info [AccountInfo<'info>],data:&[u8])->anchor_lang::solana_program::entrypoint::ProgramResult {
 if accounts.len()!=1{return Err(ProgramError::Custom(6000))}
 if data.len()!=24{return Err(ProgramError::Custom(6004))}
 entry(program_id,accounts,data)
}
#[program]
pub mod anchor_bounded_bench {
 use super::*;
 #[instruction(discriminator=[184,167,8,246,65,96,167,23])]
 pub fn swap(ctx:Context<Swap>,amount:u64,minimum:u64)->Result<()> {
  let p=&mut ctx.accounts.pool;
  if p.reserve_x==0||p.reserve_y==0||p.reserve_x>1000000000||p.reserve_y>1000000000{return Err(ProgramError::Custom(2).into())}
  if amount==0||amount>1000000||p.reserve_x+amount>1000000000{return Err(ProgramError::Custom(3).into())}
  if p.swaps==u64::MAX{return Err(ProgramError::Custom(4).into())}
  let effective=amount*9970;
  let out=p.reserve_y*effective/(p.reserve_x*10000+effective);
  if out==0||out<minimum{return Err(ProgramError::Custom(5).into())}
  p.reserve_x+=amount;p.reserve_y-=out;p.swaps+=1;Ok(())
 }
}
#[derive(Accounts)]
pub struct Swap<'info> {
 #[account(mut, constraint=pool.to_account_info().data_len()==32, constraint=!pool.to_account_info().executable)]
 pub pool:Account<'info,Pool>,
}
#[account(discriminator=[33,65,37,242,140,68,141,192])]
pub struct Pool {pub reserve_x:u64,pub reserve_y:u64,pub swaps:u64}
