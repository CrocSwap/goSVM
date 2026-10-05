use std::{collections::HashSet,env,fs};
use serde_json::Value;
use solana_program_test::{processor,ProgramTest,ProgramTestContext};
use solana_account::Account;
use solana_instruction::{Instruction,AccountMeta,error::InstructionError};
use solana_pubkey::Pubkey;
use solana_keypair::Keypair;
use solana_signer::Signer;
use solana_transaction::Transaction;
use solana_transaction_error::TransactionError;

fn bytes(v:&Value,key:&str)->Vec<u8>{v[key].as_array().unwrap().iter().map(|x|x.as_u64().unwrap() as u8).collect()}
// Direct reference checks supplement the native bank/transaction tests below.
// Both SBF images are verified independently in the shared external validator.
fn native_check(id:Pubkey,keys:&[Pubkey;4],c:&Value) {
    use solana_program::{account_info::AccountInfo,program_error::ProgramError};
    let m=c["mutation"].as_str().unwrap();
    let foreign=Pubkey::new_unique();let system=Pubkey::default();
    let mut state=bytes(c,"state");let mut cfg=bytes(c,"config");let mut payload=bytes(c,"payload");let mut actor=vec![];
    let(mut l0,mut l1,mut l2,mut l3)=(10_000_000,10_000_000,10_000_000,10_000_000);
    let mut a=vec![
        AccountInfo::new(&keys[0],false,m!="readonly",&mut l0,&mut state,if m=="wrong-owner"{&foreign}else{&id},false,0),
        AccountInfo::new(&keys[1],m!="no-signer",false,&mut l1,&mut actor,&system,false,0),
        AccountInfo::new(&keys[2],false,false,&mut l2,&mut cfg,if m=="config-owner"{&foreign}else{&id},false,0),
        AccountInfo::new(&keys[3],false,false,&mut l3,&mut payload,if m=="payload-owner"{&foreign}else{&id},false,0),
    ];
    let n=c["accounts"].as_u64().unwrap() as usize;if n<=4{a.truncate(n)}else{a.push(a[0].clone())}
    let got=protocol_bench::process_instruction(&id,&a,&bytes(c,"instruction"));
    let code=c["code"].as_u64().unwrap();
    if code==0{got.unwrap()}else{assert_eq!(got,Err(ProgramError::Custom(code as u32)))}
    drop(a);
    if code==0{assert_eq!(state,bytes(c,"expected"))}
}
fn user()->Keypair {
    let seed=[0x9d,0x61,0xb1,0x9d,0xef,0xfd,0x5a,0x60,0xba,0x84,0x4a,0xf4,0x92,0xec,0x2c,0xc4,0x44,0x49,0xc5,0x69,0x7b,0x32,0x69,0x19,0x70,0x3b,0xac,0x03,0x1c,0xae,0x7f,0x60];
    let public=[0xd7,0x5a,0x98,0x01,0x82,0xb1,0x0a,0xb7,0xd5,0x4b,0xfe,0xd3,0xc9,0x64,0x07,0x3a,0x0e,0xe1,0x72,0xf3,0xda,0xa6,0x23,0x25,0xaf,0x02,0x1a,0x68,0xf7,0x07,0x51,0x1a];
    Keypair::from_bytes(&[seed,public].concat()).unwrap()
}
fn setup(pt:&mut ProgramTest,id:Pubkey,operator:Pubkey,c:&Value)->[Pubkey;4] {
    let mut keys=[Pubkey::new_unique(),operator,Pubkey::new_unique(),Pubkey::new_unique()];
    let mutation=c["mutation"].as_str().unwrap();if mutation=="alias"{keys[3]=keys[2]}
    let mut written=HashSet::new();
    for (j,name) in [(0,"state"),(2,"config"),(3,"payload")] {
        if !written.insert(keys[j]){continue}
        let wrong=(j==0&&mutation=="wrong-owner")||(j==2&&mutation=="config-owner")||(j==3&&mutation=="payload-owner");
        pt.add_account(keys[j],Account{lamports:10_000_000,data:bytes(c,name),owner:if wrong{Pubkey::new_unique()}else{id},executable:false,rent_epoch:0});
    }
    keys
}
fn ix(id:Pubkey,keys:&[Pubkey;4],c:&Value)->Instruction {
    let m=c["mutation"].as_str().unwrap();
    let accounts=(0..c["accounts"].as_u64().unwrap() as usize).map(|i|{
        let j=i%4;
        if j==0&&m!="readonly" {AccountMeta::new(keys[j],false)}else{AccountMeta::new_readonly(keys[j],j==1&&m!="no-signer")}
    }).collect();
    Instruction{program_id:id,accounts,data:bytes(c,"instruction")}
}
async fn submit(ctx:&mut ProgramTestContext,operator:&Keypair,instructions:&[Instruction],code:u64,index:u8){
    let needs_user=instructions.iter().any(|i|i.accounts.iter().any(|m|m.pubkey==operator.pubkey()&&m.is_signer));
    let mut signers:Vec<&Keypair>=vec![&ctx.payer];if needs_user{signers.push(operator)}
    let tx=Transaction::new_signed_with_payer(instructions,Some(&ctx.payer.pubkey()),&signers,ctx.last_blockhash);
    let result=ctx.banks_client.process_transaction(tx).await;
    if code==0 {result.unwrap()}else{assert_eq!(result.unwrap_err().unwrap(),TransactionError::InstructionError(index,InstructionError::Custom(code as u32)))}
}
pub async fn run(group:usize) {
    let fixture:Value=serde_json::from_slice(&fs::read(env::var("PROTOCOL_FIXTURES").expect("run scripts/protocol_bench.py")).unwrap()).unwrap();
    let cases=fixture["cases"].as_array().unwrap();
    for name in ["protocol_native"] {
        let id=Pubkey::new_unique();let operator=user();
        // ProgramTest 2.3.9's SBFv3 parser predates platform-tools v1.51's
        // ELF layout. Use its native processor mode; never silently substitute
        // native execution for a test that claims to verify SBF.
        let mut pt=ProgramTest::new(name,id,processor!(protocol_bench::process_instruction));pt.prefer_bpf(false);
        pt.add_account(operator.pubkey(),Account{lamports:10_000_000,owner:Pubkey::default(),..Account::default()});
        let mut work=Vec::new();
        for (i,c) in cases.iter().enumerate(){if i%12==group {work.push((i,setup(&mut pt,id,operator.pubkey(),c)))}}
        let sequence=if group==0{Some(setup(&mut pt,id,operator.pubkey(),&cases[0]))}else{None};
        let rollback=if group==0{Some(setup(&mut pt,id,operator.pubkey(),&cases[0]))}else{None};
        let mut ctx=pt.start_with_context().await;
        for (i,keys) in work {
            let c=&cases[i];native_check(id,&keys,c);submit(&mut ctx,&operator,&[ix(id,&keys,c)],c["code"].as_u64().unwrap(),0).await;
            let actual=ctx.banks_client.get_account(keys[0]).await.unwrap().unwrap().data;
            assert_eq!(actual,bytes(c,"expected"),"{} {}",name,c["name"]);
        }
        if let Some(keys)=sequence {
            for step in fixture["lifecycle"].as_array().unwrap(){
                let c=&cases[step.as_u64().unwrap() as usize];submit(&mut ctx,&operator,&[ix(id,&keys,c)],0,0).await;
                assert_eq!(ctx.banks_client.get_account(keys[0]).await.unwrap().unwrap().data,bytes(c,"expected"));
            }
        }
        if let Some(keys)=rollback {
            let first=ix(id,&keys,&cases[0]);let mut second=ix(id,&keys,&cases[1]);second.data[8..16].copy_from_slice(&8u64.to_le_bytes());
            submit(&mut ctx,&operator,&[first,second],201,1).await;
            assert_eq!(ctx.banks_client.get_account(keys[0]).await.unwrap().unwrap().data,bytes(&cases[0],"state"));
        }
    }
}
