use anchor_lang::prelude::*;
use anchor_spl::token::{self, Token, TokenAccount, Transfer};

declare_id!("E2aHGEfpjCoNpNr8q39oYeCRmrqFhMDwPMKZDyfuscR");

// Anchor normally permits trailing instruction bytes and remaining accounts.
// This narrow envelope guard matches the experiment's exact-count/length policy;
// all typed account parsing, constraints, dispatch, serialization and CPI remain Anchor.
anchor_lang::solana_program::entrypoint!(checked_entry);
fn checked_entry<'info>(program_id: &Pubkey, accounts: &'info [AccountInfo<'info>], data: &[u8]) -> anchor_lang::solana_program::entrypoint::ProgramResult {
    if accounts.len() != 8 { return Err(ProgramError::Custom(101)); }
    if data.len() != 24 { return Err(ProgramError::Custom(102)); }
    entry(program_id, accounts, data)
}

#[program]
pub mod anchor_token_bench {
    use super::*;
    pub fn swap(ctx: Context<Swap>, amount: u64, minimum: u64) -> Result<()> {
        let a = ctx.accounts;
        // Also disallow aliases involving readonly accounts, as both baselines do.
        let keys = [a.pool.key(),a.user.key(),a.user_x.key(),a.vault_x.key(),a.vault_y.key(),a.user_y.key(),a.authority.key(),a.token_program.key()];
        for i in 0..8 {for j in 0..i {require!(keys[i] != keys[j], BenchError::Alias);}}
        let x=a.vault_x.amount; let y=a.vault_y.amount;
        require!(amount!=0 && x!=0 && y!=0 && amount<=u64::MAX-x && a.user_x.amount>=amount,BenchError::Input);
        let fee=((amount as u128*30+9999)/10000) as u64;
        let net=amount-fee;
        let out=((y as u128*net as u128)/(x+net) as u128) as u64;
        require!(out!=0 && out>=minimum && out<=u64::MAX-a.user_y.amount,BenchError::Output);
        require!(a.pool.swaps!=u64::MAX,BenchError::Counter);
        token::transfer(CpiContext::new(a.token_program.to_account_info(),Transfer {
            from:a.user_x.to_account_info(),to:a.vault_x.to_account_info(),authority:a.user.to_account_info(),
        }),amount)?;
        let pool_key=a.pool.key(); let bump=[a.pool.bump]; let seeds:&[&[u8]]=&[pool_key.as_ref(),&bump];
        token::transfer(CpiContext::new_with_signer(a.token_program.to_account_info(),Transfer {
            from:a.vault_y.to_account_info(),to:a.user_y.to_account_info(),authority:a.authority.to_account_info(),
        },&[seeds]),out)?;
        a.pool.swaps+=1;
        Ok(())
    }
}

#[derive(Accounts)]
pub struct Swap<'info> {
    #[account(mut, has_one=vault_x, has_one=vault_y,
        constraint=pool.to_account_info().data_len()==145 @ BenchError::Layout,
        constraint=!pool.to_account_info().executable @ BenchError::Flags,
        constraint=pool.mint_x!=pool.mint_y @ BenchError::Mint)]
    pub pool: Account<'info, Pool>,
    #[account(constraint=!user.to_account_info().executable @ BenchError::Flags)]
    pub user: Signer<'info>,
    #[account(mut, token::mint=pool.mint_x, token::authority=user,
        constraint=user_x.to_account_info().data_len()==165 && !user_x.to_account_info().executable && user_x.is_native.is_none() @ BenchError::Layout)]
    pub user_x: Account<'info, TokenAccount>,
    #[account(mut, token::mint=pool.mint_x, token::authority=authority,
        constraint=vault_x.to_account_info().data_len()==165 && !vault_x.to_account_info().executable && vault_x.is_native.is_none() @ BenchError::Layout)]
    pub vault_x: Account<'info, TokenAccount>,
    #[account(mut, token::mint=pool.mint_y, token::authority=authority,
        constraint=vault_y.to_account_info().data_len()==165 && !vault_y.to_account_info().executable && vault_y.is_native.is_none() @ BenchError::Layout)]
    pub vault_y: Account<'info, TokenAccount>,
    #[account(mut, token::mint=pool.mint_y, token::authority=user,
        constraint=user_y.to_account_info().data_len()==165 && !user_y.to_account_info().executable && user_y.is_native.is_none() @ BenchError::Layout)]
    pub user_y: Account<'info, TokenAccount>,
    /// CHECK: Identity is established by the PDA seeds; it has no data to decode.
    #[account(seeds=[pool.key().as_ref()],bump=pool.bump)]
    pub authority: UncheckedAccount<'info>,
    pub token_program: Program<'info, Token>,
}

#[account]
pub struct Pool {pub vault_x:Pubkey,pub vault_y:Pubkey,pub mint_x:Pubkey,pub mint_y:Pubkey,pub bump:u8,pub swaps:u64}

#[error_code]
pub enum BenchError {Layout,Flags,Alias,Mint,Input,Output,Counter}
