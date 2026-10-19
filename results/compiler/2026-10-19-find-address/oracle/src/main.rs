// Prints find_program_address vectors from solana-pubkey 3.0.0 as JSON:
// program, seeds, the address (null when none) and the bump.
use solana_pubkey::Pubkey;
use sha2::{Digest, Sha256};

fn hexs(b: &[u8]) -> String { b.iter().map(|x| format!("{:02x}", x)).collect() }

fn main() {
    let mut cases: Vec<(Pubkey, Vec<Vec<u8>>)> = Vec::new();
    // Deterministic pseudo-random seeds: SHA-256 chains.
    let mut state = Sha256::digest(b"gosvm find_program_address vectors").to_vec();
    let mut next = |n: usize| -> Vec<u8> {
        let mut out = Vec::new();
        while out.len() < n { state = Sha256::digest(&state).to_vec(); out.extend_from_slice(&state); }
        out.truncate(n); out
    };
    for i in 0..400u32 {
        let program = Pubkey::new_from_array(next(32).try_into().unwrap());
        let count = (i % 16) as usize; // 0..15 seeds
        let mut seeds = Vec::new();
        for j in 0..count { let len = (next(1)[0] as usize + j) % 33; seeds.push(next(len)); }
        cases.push((program, seeds));
    }
    // Sixteen seeds leave no room for the bump.
    cases.push((Pubkey::new_from_array([3; 32]), vec![vec![1u8]; 16]));
    println!("[");
    for (i, (program, seeds)) in cases.iter().enumerate() {
        let refs: Vec<&[u8]> = seeds.iter().map(|s| s.as_slice()).collect();
        let joined: Vec<String> = seeds.iter().map(|s| format!("\"{}\"", hexs(s))).collect();
        let found = match Pubkey::try_find_program_address(&refs, program) {
            Some((a, b)) => format!("\"address\": \"{}\", \"bump\": {}", hexs(a.as_ref()), b),
            None => "\"address\": null, \"bump\": 0".to_string(),
        };
        let comma = if i + 1 == cases.len() { "" } else { "," };
        println!("  {{\"program\": \"{}\", \"seeds\": [{}], {}}}{}", hexs(program.as_ref()), joined.join(", "), found, comma);
    }
    println!("]");
}
