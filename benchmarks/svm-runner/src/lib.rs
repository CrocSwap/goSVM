//! Compatibility spike: the small local RPC surface used by goSVM's fixtures.
//! This is a persistent test sidecar, not an implementation of Solana RPC.
use agave_feature_set::FeatureSet;
use base64::{engine::general_purpose::STANDARD, Engine};
use litesvm::LiteSVM;
use serde_json::{json, Value};
use solana_account::Account;
use solana_clock::Clock;
use solana_pubkey::Pubkey;
use solana_rent::Rent;
use solana_transaction::versioned::VersionedTransaction;
use std::{collections::HashMap, fs, path::PathBuf, str::FromStr, time::Instant};

const VERSION: &str = "gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)";
const HISTORY_CAPACITY: usize = 4096;

fn clone_vm(svm: &LiteSVM) -> LiteSVM {
    // IndexMap::clone can lose spare capacity (especially for an empty map).
    // LiteSVM uses that capacity as its transaction-history limit. Restore it
    // explicitly so a reset cannot silently disable duplicate detection.
    svm.clone().with_transaction_history(HISTORY_CAPACITY)
}

// Validate a complete typed value before touching either the account or cache.
// Arbitrary sysvar account writes can desynchronize those two views.
fn exact_fields(value: &Value, fields: &[&str]) -> Result<(), String> {
    let object = value.as_object().ok_or("require sysvar object")?;
    if object.len() != fields.len() || fields.iter().any(|field| !object.contains_key(*field)) {
        return Err(format!(
            "require exactly these fields: {}",
            fields.join(", ")
        ));
    }
    Ok(())
}
fn set_sysvars(svm: &mut LiteSVM, value: &Value) -> Result<(), String> {
    let object = value.as_object().ok_or("require sysvars object")?;
    if object.keys().any(|key| key != "clock" && key != "rent") {
        return Err("only clock and rent controls are supported".into());
    }
    let clock = if let Some(value) = object.get("clock") {
        exact_fields(
            value,
            &[
                "slot",
                "epoch_start_timestamp",
                "epoch",
                "leader_schedule_epoch",
                "unix_timestamp",
            ],
        )?;
        Some(serde_json::from_value::<Clock>(value.clone()).map_err(|e| e.to_string())?)
    } else {
        None
    };
    let rent = if let Some(value) = object.get("rent") {
        exact_fields(
            value,
            &[
                "lamports_per_byte_year",
                "exemption_threshold",
                "burn_percent",
            ],
        )?;
        let rent = serde_json::from_value::<Rent>(value.clone()).map_err(|e| e.to_string())?;
        if !rent.exemption_threshold.is_finite()
            || rent.exemption_threshold < 0.0
            || rent.burn_percent > 100
        {
            return Err(
                "rent requires finite nonnegative threshold and burn_percent <= 100".into(),
            );
        }
        Some(rent)
    } else {
        None
    };
    if let Some(clock) = clock {
        svm.set_sysvar(&clock);
    }
    if let Some(rent) = rent {
        svm.set_sysvar(&rent);
    }
    Ok(())
}
fn sysvars(svm: &LiteSVM) -> Value {
    json!({"clock":svm.get_sysvar::<Clock>(), "rent":svm.get_sysvar::<Rent>()})
}

fn selected_features(ids: &[String]) -> Result<FeatureSet, String> {
    let known = FeatureSet::all_enabled();
    let mut selected = FeatureSet::default();
    for id in ids {
        let key = Pubkey::from_str(id).map_err(|e| e.to_string())?;
        if !known.is_active(&key) {
            return Err(format!("runner does not know validator feature {id}"));
        }
        selected.activate(&key, 0);
    }
    Ok(selected)
}

fn address(v: &Value) -> Result<Pubkey, String> {
    Pubkey::from_str(v.as_str().ok_or("expected base58 address")?).map_err(|e| e.to_string())
}
fn account_json(a: &Account) -> Value {
    json!({"data":[STANDARD.encode(&a.data),"base64"], "owner":a.owner.to_string(),
           "lamports":a.lamports,"executable":a.executable,"rentEpoch":a.rent_epoch})
}
fn account_from_json(v: &Value) -> Result<Account, String> {
    if v["data"][1] != "base64" {
        return Err("require base64 account data".into());
    }
    Ok(Account {
        data: STANDARD
            .decode(v["data"][0].as_str().ok_or("missing account data")?)
            .map_err(|e| e.to_string())?,
        owner: address(&v["owner"])?,
        lamports: v["lamports"].as_u64().ok_or("missing lamports")?,
        executable: v["executable"].as_bool().unwrap_or(false),
        rent_epoch: v["rentEpoch"].as_u64().unwrap_or(0),
    })
}
fn seed_accounts(svm: &mut LiteSVM, entries: &Value) -> Result<(), String> {
    let sysvar_owner = Pubkey::from_str("Sysvar1111111111111111111111111111111111111").unwrap();
    for entry in entries.as_array().ok_or("require accounts array")? {
        let key = address(&entry["pubkey"])?;
        let account = account_from_json(&entry["account"])?;
        if account.executable
            || account.owner == sysvar_owner
            || svm
                .get_account(&key)
                .is_some_and(|a| a.executable || a.owner == sysvar_owner)
        {
            return Err("account fixtures cannot modify programs or sysvars".into());
        }
        svm.set_account(key, account).map_err(|e| e.to_string())?;
    }
    Ok(())
}
fn tx(params: &Value) -> Result<VersionedTransaction, String> {
    let bytes = STANDARD
        .decode(params[0].as_str().ok_or("missing transaction")?)
        .map_err(|e| e.to_string())?;
    bincode::deserialize(&bytes).map_err(|e| e.to_string())
}

// The RPC-shaped returnData field follows the fixture convention: empty is
// null; nonempty contains exact bytes and the program that most recently set it.
fn return_data_json(program: &Pubkey, data: &[u8]) -> Value {
    if data.is_empty() {
        Value::Null
    } else {
        json!({"programId":program.to_string(),"data":[STANDARD.encode(data),"base64"]})
    }
}

struct Runner {
    svm: LiteSVM,
    statuses: HashMap<String, Value>,
    transactions: HashMap<String, Value>,
    startup_us: u128,
    execution_us: u128,
    requests: u64,
    features: Vec<String>,
    feature_policy: &'static str,
}
impl Runner {
    fn new(config: &Value) -> Result<Self, String> {
        let started = Instant::now();
        let (feature_set, feature_policy) = if config["features"].is_null() {
            (FeatureSet::all_enabled(), "all_enabled")
        } else {
            let ids: Vec<String> =
                serde_json::from_value(config["features"].clone()).map_err(|e| e.to_string())?;
            (selected_features(&ids)?, "validator_fixture_file")
        };
        let mut features: Vec<String> = feature_set
            .active()
            .keys()
            .map(ToString::to_string)
            .collect();
        features.sort();
        let mut svm = LiteSVM::default()
            .with_feature_set(feature_set)
            .with_builtins()
            .with_lamports(1_000_000_000_000_000)
            .with_sysvars()
            .with_default_programs()
            .with_transaction_history(HISTORY_CAPACITY)
            .with_sigverify(true)
            .with_blockhash_check(true);
        let programs = config["programs"]
            .as_array()
            .ok_or("require programs array")?;
        // Explicit supplied images override bundled programs before fixtures load.
        for program in programs {
            let image = STANDARD
                .decode(program["elf"].as_str().ok_or("require base64 ELF")?)
                .map_err(|e| e.to_string())?;
            svm.add_program(address(&program["id"])?, &image)
                .map_err(|e| e.to_string())?;
        }
        seed_accounts(&mut svm, &config["accounts"])?;
        svm.set_account(
            address(&config["payer"])?,
            Account {
                lamports: 1_000_000_000_000,
                data: vec![],
                owner: Pubkey::default(),
                executable: false,
                rent_epoch: 0,
            },
        )
        .map_err(|e| e.to_string())?;
        if let Some(value) = config.get("sysvars") {
            set_sysvars(&mut svm, value)?;
        }
        Ok(Self {
            svm,
            statuses: HashMap::new(),
            transactions: HashMap::new(),
            startup_us: started.elapsed().as_micros(),
            execution_us: 0,
            requests: 0,
            features,
            feature_policy,
        })
    }

    fn rpc(&mut self, request: &Value) -> Value {
        match self.call(request["method"].as_str().unwrap_or(""), &request["params"]) {
            Ok(result) => json!({"jsonrpc":"2.0","id":request["id"],"result":result}),
            Err(error) => {
                json!({"jsonrpc":"2.0","id":request["id"],"error":{"code":-32602,"message":error}})
            }
        }
    }
    fn call(&mut self, method: &str, p: &Value) -> Result<Value, String> {
        self.requests += 1;
        match method {
            "getSlot" => Ok(json!(2)), // legacy HTTP readiness marker; use get_sysvars for clock
            "getLatestBlockhash" => {
                Ok(json!({"value":{"blockhash":self.svm.latest_blockhash().to_string()}}))
            }
            "getAccountInfo" => Ok(
                json!({"value":self.svm.get_account(&address(&p[0])?).as_ref().map(account_json)}),
            ),
            "simulateTransaction" => {
                if p[1]["encoding"] != "base64" || p[1]["sigVerify"] != true {
                    return Err(
                        "spike requires base64 transactions and signature verification".into(),
                    );
                }
                let transaction = tx(p)?;
                let start = Instant::now();
                let result = self.svm.simulate_transaction(transaction);
                self.execution_us += start.elapsed().as_micros();
                let (meta, error, post) = match result {
                    Ok(info) => (info.meta, Value::Null, info.post_accounts),
                    Err(info) => (info.meta, json!(info.err), vec![]),
                };
                let mut accounts = vec![];
                if let Some(addresses) = p[1]["accounts"]["addresses"].as_array() {
                    for value in addresses {
                        let key = address(value)?;
                        // Failed simulations expose no post-state, like our validator fixtures.
                        let mut a: Option<Account> = post
                            .iter()
                            .find(|(k, _)| *k == key)
                            .map(|(_, a)| a.clone().into());
                        // General fixtures can watch unchanged accounts outside
                        // the message; successful simulation leaves those intact.
                        if a.is_none() && error.is_null() {
                            a = self.svm.get_account(&key);
                        }
                        accounts.push(a.as_ref().map(account_json));
                    }
                }
                Ok(
                    json!({"value":{"err":error,"unitsConsumed":meta.compute_units_consumed,
                           "logs":meta.logs,"accounts":accounts,
                           "returnData":return_data_json(&meta.return_data.program_id,&meta.return_data.data)}}),
                )
            }
            "sendTransaction" => {
                if p[1]["encoding"] != "base64" || p[1]["skipPreflight"] != true {
                    return Err("spike requires base64 and skipPreflight".into());
                }
                let transaction = tx(p)?;
                let signature = transaction
                    .signatures
                    .first()
                    .ok_or("missing signature")?
                    .to_string();
                let start = Instant::now();
                let result = self.svm.send_transaction(transaction);
                self.execution_us += start.elapsed().as_micros();
                let (meta, error) = match result {
                    Ok(meta) => (meta, Value::Null),
                    Err(info) => (info.meta, json!(info.err)),
                };
                let metadata = json!({"meta":{"err":error,"logMessages":meta.logs,
                    "computeUnitsConsumed":meta.compute_units_consumed,
                    "returnData":return_data_json(&meta.return_data.program_id,&meta.return_data.data)}});
                self.statuses.insert(signature.clone(), error);
                self.transactions.insert(signature.clone(), metadata);
                Ok(json!(signature))
            }
            // Synchronous test metadata only: no ledger or commitment semantics.
            "getTransaction" => {
                let signature = p[0].as_str().ok_or("missing signature")?;
                Ok(self
                    .transactions
                    .get(signature)
                    .cloned()
                    .unwrap_or(Value::Null))
            }
            "getSignatureStatuses" => {
                let signatures = p[0].as_array().ok_or("missing signatures")?;
                let values: Vec<Value> = signatures
                    .iter()
                    .map(|s| {
                        self.statuses
                            .get(s.as_str().unwrap_or(""))
                            .map(|e| json!({"err":e,"confirmationStatus":"confirmed"}))
                            .unwrap_or(Value::Null)
                    })
                    .collect();
                Ok(json!({"value":values}))
            }
            "gosvmRuntimeInfo" => Ok(
                json!({"version":VERSION,"feature_policy":self.feature_policy,
                "active_features":self.features,"startup_us":self.startup_us,
                "execution_us":self.execution_us,"requests":self.requests,
                "sysvars":sysvars(&self.svm),
                "execution_semantics":"synchronous in-memory test outcomes; no bank or consensus"}),
            ),
            _ => Err(format!("unsupported spike method: {method}")),
        }
    }
}

pub fn serve_http(args: Vec<String>) -> Result<(), Box<dyn std::error::Error>> {
    let started = Instant::now();
    if args == ["--version"] {
        println!("{VERSION}");
        return Ok(());
    }
    let mut port = None;
    let mut mint = None;
    let mut account_dir = None;
    let mut programs: Vec<(Pubkey, PathBuf)> = vec![];
    let mut i = 0;
    while i < args.len() {
        let value = |n: usize| args.get(i + n).ok_or("missing argument");
        match args[i].as_str() {
            "--quiet" => i += 1,
            "--ledger" | "--faucet-port" => {
                value(1)?;
                i += 2;
            }
            "--bind-address" => {
                if value(1)? != "127.0.0.1" {
                    return Err("loopback only".into());
                }
                i += 2;
            }
            "--rpc-port" => {
                port = Some(value(1)?.parse::<u16>()?);
                i += 2;
            }
            "--mint" => {
                mint = Some(Pubkey::from_str(value(1)?)?);
                i += 2;
            }
            "--account-dir" => {
                account_dir = Some(PathBuf::from(value(1)?));
                i += 2;
            }
            "--bpf-program" => {
                programs.push((Pubkey::from_str(value(1)?)?, PathBuf::from(value(2)?)));
                i += 3;
            }
            _ => return Err(format!("unsupported spike argument: {}", args[i]).into()),
        }
    }
    let features: Value = match std::env::var("GOSVM_TEST_FEATURES") {
        Ok(path) => serde_json::from_slice(&fs::read(path)?)?,
        Err(_) => Value::Null,
    };
    let mut accounts = vec![];
    for entry in fs::read_dir(account_dir.ok_or("require --account-dir")?)? {
        let path = entry?.path();
        if path.extension().and_then(|s| s.to_str()) == Some("json") {
            accounts.push(serde_json::from_slice::<Value>(&fs::read(path)?)?);
        }
    }
    let mut images = vec![];
    if let Ok(path) = std::env::var("GOSVM_TEST_TOKEN_ELF") {
        images.push(json!({"id":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
                          "elf":STANDARD.encode(fs::read(path)?)}));
    }
    for (key, path) in programs {
        images.push(json!({"id":key.to_string(),"elf":STANDARD.encode(fs::read(path)?)}));
    }
    let config = json!({"features":features,"accounts":accounts,"programs":images,
                       "payer":mint.ok_or("require --mint")?.to_string()});
    let mut runner = Runner::new(&config)?;
    runner.startup_us = started.elapsed().as_micros();
    let server = tiny_http::Server::http(("127.0.0.1", port.ok_or("require --rpc-port")?))
        .map_err(|e| e.to_string())?;
    // Optional immutable replay inputs capture the fixture adapter, not a wallet.
    let mut trace = match std::env::var("GOSVM_RUNNER_TRACE") {
        Ok(path) => {
            let mut path = PathBuf::from(path);
            if path.is_dir() {
                path = path.join(format!("trace-{}.jsonl", std::process::id()));
            }
            let mut file = fs::OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(path)?;
            writeln!(file, "{}", json!({"schema":1,"config":config}))?;
            Some(file)
        }
        Err(_) => None,
    };
    for mut request in server.incoming_requests() {
        let mut bytes = vec![];
        request
            .as_reader()
            .take(4 * 1024 * 1024)
            .read_to_end(&mut bytes)?;
        let value = serde_json::from_slice::<Value>(&bytes);
        let response = match &value {
            Ok(v) => runner.rpc(v),
            Err(e) => {
                json!({"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":e.to_string()}})
            }
        };
        if let (Some(file), Ok(v)) = (&mut trace, value) {
            if v["method"] != "gosvmRuntimeInfo" {
                writeln!(file, "{}", json!({"request":v,"response":response}))?;
            }
        }
        request.respond(
            tiny_http::Response::from_string(response.to_string()).with_header(
                tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
            ),
        )?;
    }
    Ok(())
}

use std::io::{Read, Write};

// The same versioned session envelope is used over stdio and the experimental
// embedded C ABI. A batch is ordered; each transaction retains its own atomicity.
#[derive(Default)]
struct Session {
    runner: Option<Runner>,
    checkpoint: Option<(LiteSVM, HashMap<String, Value>, HashMap<String, Value>)>,
}
impl Session {
    fn request(&mut self, message: &Value) -> Value {
        let id = message["id"].clone();
        let result = self.execute(message);
        match result {
            Ok(value) => json!({"schema":1,"id":id,"result":value}),
            Err(error) => json!({"schema":1,"id":id,"error":{"message":error}}),
        }
    }
    fn execute(&mut self, message: &Value) -> Result<Value, String> {
        if message["schema"] != 1 || !message["id"].is_u64() {
            return Err("require schema 1 and uint64 id".into());
        }
        match message["op"].as_str().ok_or("require op")? {
            "init" => {
                if self.runner.is_some() {
                    return Err("session already initialized".into());
                }
                let runner = Runner::new(&message["config"])?;
                self.checkpoint = Some((
                    clone_vm(&runner.svm),
                    runner.statuses.clone(),
                    runner.transactions.clone(),
                ));
                self.runner = Some(runner);
                Ok(json!({"ready":true}))
            }
            "get_sysvars" => Ok(sysvars(
                &self.runner.as_ref().ok_or("session not initialized")?.svm,
            )),
            "set_sysvars" => {
                set_sysvars(
                    &mut self.runner.as_mut().ok_or("session not initialized")?.svm,
                    &message["sysvars"],
                )?;
                Ok(json!({"done":true}))
            }
            "snapshot" => {
                let runner = self.runner.as_ref().ok_or("session not initialized")?;
                self.checkpoint = Some((
                    clone_vm(&runner.svm),
                    runner.statuses.clone(),
                    runner.transactions.clone(),
                ));
                Ok(json!({"done":true}))
            }
            "reset" => {
                let runner = self.runner.as_mut().ok_or("session not initialized")?;
                let (svm, statuses, transactions) =
                    self.checkpoint.as_ref().ok_or("missing checkpoint")?;
                let mut restored = clone_vm(svm);
                if let Some(entries) = message.get("accounts") {
                    seed_accounts(&mut restored, entries)?;
                }
                // Commit only after every override validates; failed reset leaves
                // current state/statuses and the reusable checkpoint untouched.
                runner.svm = restored;
                runner.statuses = statuses.clone();
                runner.transactions = transactions.clone();
                Ok(json!({"done":true}))
            }
            "expire_blockhash" => {
                self.runner
                    .as_mut()
                    .ok_or("session not initialized")?
                    .svm
                    .expire_blockhash();
                Ok(json!({"done":true}))
            }
            "batch" => {
                let calls = message["calls"].as_array().ok_or("require calls array")?;
                if calls.len() > 4096 {
                    return Err("batch exceeds 4096 calls".into());
                }
                let runner = self.runner.as_mut().ok_or("session not initialized")?;
                Ok(Value::Array(
                    calls.iter().map(|call| runner.rpc(call)).collect(),
                ))
            }
            _ => Err("unsupported session operation".into()),
        }
    }
    fn bytes(&mut self, input: &[u8]) -> Vec<u8> {
        let response = match serde_json::from_slice::<Value>(input) {
            Ok(message) => self.request(&message),
            Err(error) => json!({"schema":1,"id":null,"error":{"message":error.to_string()}}),
        };
        serde_json::to_vec(&response).expect("JSON response serialization")
    }
}

pub fn serve_stdio() -> Result<(), Box<dyn std::error::Error>> {
    use std::io::BufRead;
    let mut input = std::io::BufReader::new(std::io::stdin().lock());
    let mut output = std::io::BufWriter::new(std::io::stdout().lock());
    let mut session = Session::default();
    loop {
        let mut line = vec![];
        let count = (&mut input)
            .take(16 * 1024 * 1024 + 1)
            .read_until(b'\n', &mut line)?;
        if count == 0 {
            return Ok(());
        }
        if count > 16 * 1024 * 1024 || line.last() != Some(&b'\n') {
            return Err("stdio frame exceeds 16 MiB or missing newline".into());
        }
        output.write_all(&session.bytes(&line))?;
        output.write_all(b"\n")?;
        output.flush()?;
    }
}

pub fn serve_http_session() -> Result<(), Box<dyn std::error::Error>> {
    let server = tiny_http::Server::http(("127.0.0.1", 0)).map_err(|e| e.to_string())?;
    let port = server
        .server_addr()
        .to_ip()
        .ok_or("require TCP listener")?
        .port();
    println!("{}", json!({"url":format!("http://127.0.0.1:{port}")}));
    std::io::stdout().flush()?;
    let mut session = Session::default();
    for mut request in server.incoming_requests() {
        let mut bytes = vec![];
        request
            .as_reader()
            .take(16 * 1024 * 1024 + 1)
            .read_to_end(&mut bytes)?;
        if bytes.len() > 16 * 1024 * 1024 {
            return Err("HTTP frame exceeds 16 MiB".into());
        }
        request.respond(
            tiny_http::Response::from_data(session.bytes(&bytes)).with_header(
                tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
            ),
        )?;
    }
    Ok(())
}

// Experimental benchmark ABI. Handles and response strings are Rust-owned;
// callers free each exactly once, serialize access, and never share Go pointers.
use std::ffi::{c_char, c_void, CStr, CString};
#[no_mangle]
pub extern "C" fn gosvm_session_new() -> *mut c_void {
    Box::into_raw(Box::new(Session::default())).cast()
}
#[no_mangle]
pub unsafe extern "C" fn gosvm_session_free(handle: *mut c_void) {
    if !handle.is_null() {
        drop(Box::from_raw(handle.cast::<Session>()));
    }
}
#[no_mangle]
pub unsafe extern "C" fn gosvm_response_free(response: *mut c_char) {
    if !response.is_null() {
        drop(CString::from_raw(response));
    }
}
#[no_mangle]
pub unsafe extern "C" fn gosvm_session_request(
    handle: *mut c_void,
    input: *const c_char,
) -> *mut c_char {
    if handle.is_null() || input.is_null() {
        return std::ptr::null_mut();
    }
    let session = &mut *handle.cast::<Session>();
    let response = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        session.bytes(CStr::from_ptr(input).to_bytes())
    }));
    let bytes = match response {
        Ok(bytes) => bytes,
        Err(_) => {
            session.runner = None;
            b"{\"schema\":1,\"id\":null,\"error\":{\"message\":\"runner panic; session reset\"}}"
                .to_vec()
        }
    };
    CString::new(bytes)
        .expect("JSON contains no NUL bytes")
        .into_raw()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn return_data_metadata_is_exact_and_reset_restores_history() {
        let key = Pubkey::new_unique();
        assert_eq!(return_data_json(&key, &[]), Value::Null);
        assert_eq!(
            return_data_json(&key, &[0, 1, 255]),
            json!({"programId":key.to_string(),"data":["AAH/","base64"]})
        );
        let mut session = Session::default();
        let request = |op: &str| json!({"schema":1,"id":0,"op":op});
        let payer = Pubkey::new_unique();
        assert!(session.request(&json!({"schema":1,"id":0,"op":"init",
            "config":{"payer":payer.to_string(),"programs":[],"accounts":[]}}))["result"]["ready"]
            .as_bool()
            .unwrap());
        session.runner.as_mut().unwrap().transactions.insert(
            "one".into(),
            json!({"meta":{"returnData":return_data_json(&key,&[0,1,255])}}),
        );
        session.request(&request("snapshot"));
        session
            .runner
            .as_mut()
            .unwrap()
            .transactions
            .insert("two".into(), Value::Null);
        session.request(&request("reset"));
        let runner = session.runner.as_mut().unwrap();
        assert_eq!(
            runner.call("getTransaction", &json!(["two"])).unwrap(),
            Value::Null
        );
        assert_eq!(
            runner.call("getTransaction", &json!(["one"])).unwrap()["meta"]["returnData"],
            return_data_json(&key, &[0, 1, 255])
        );
    }

    #[test]
    fn reject_unknown_features_and_select_only_requested_features() {
        assert!(selected_features(&[Pubkey::default().to_string()]).is_err());
        assert!(selected_features(&["invalid".to_string()]).is_err());
        let known = FeatureSet::all_enabled();
        let key = *known.active().keys().next().unwrap();
        let selected = selected_features(&[key.to_string()]).unwrap();
        assert!(selected.is_active(&key));
        assert_eq!(selected.active().len(), 1);
    }

    #[test]
    fn session_rejects_bad_frames_and_uninitialized_execution() {
        let mut s = Session::default();
        assert!(s.request(&json!({"schema":2,"id":0,"op":"init"}))["error"].is_object());
        assert!(
            s.request(&json!({"schema":1,"id":0,"op":"batch","calls":[]}))["error"].is_object()
        );
        let response: Value = serde_json::from_slice(&s.bytes(b"not JSON")).unwrap();
        assert!(response["error"].is_object());
        for op in [
            "get_sysvars",
            "set_sysvars",
            "snapshot",
            "reset",
            "expire_blockhash",
        ] {
            assert!(s.request(&json!({"schema":1,"id":0,"op":op}))["error"].is_object());
        }
    }

    #[test]
    fn sysvar_validation_is_atomic_and_checkpoint_is_reusable() {
        let mut session = Session::default();
        let request = |op: &str| json!({"schema":1,"id":0,"op":op});
        let mut init = request("init");
        init["config"] = json!({"features":[],"accounts":[],"programs":[],"payer":Pubkey::new_unique().to_string()});
        assert_eq!(session.request(&init)["result"]["ready"], true);
        let original = session.request(&request("get_sysvars"))["result"].clone();
        let mut update = request("set_sysvars");
        let mut changed = original.clone();
        changed["clock"]["slot"] = json!(u64::MAX);
        changed["clock"]["unix_timestamp"] = json!(i64::MIN);
        changed["rent"]["burn_percent"] = json!(101);
        update["sysvars"] = changed.clone();
        assert!(session.request(&update)["error"].is_object());
        assert_eq!(session.request(&request("get_sysvars"))["result"], original);
        changed["rent"]["burn_percent"] = json!(0);
        update["sysvars"] = changed.clone();
        assert_eq!(session.request(&update)["result"]["done"], true);
        assert_eq!(session.request(&request("get_sysvars"))["result"], changed);
        assert_eq!(
            session.request(&request("snapshot"))["result"]["done"],
            true
        );
        for _ in 0..2 {
            update["sysvars"] = original.clone();
            assert_eq!(session.request(&update)["result"]["done"], true);
            assert_eq!(session.request(&request("reset"))["result"]["done"], true);
            assert_eq!(session.request(&request("get_sysvars"))["result"], changed);
        }
        for invalid in [
            json!({"clock":null}),
            json!({"clock":{"slot":1}}),
            json!({"unknown":{}}),
            json!({"rent":{"lamports_per_byte_year":1,"exemption_threshold":-1.0,"burn_percent":0}}),
        ] {
            update["sysvars"] = invalid;
            assert!(session.request(&update)["error"].is_object());
            assert_eq!(session.request(&request("get_sysvars"))["result"], changed);
        }
    }

    #[test]
    fn initialization_rejects_raw_sysvar_fixtures() {
        let mut session = Session::default();
        let mut init = json!({"schema":1,"id":0,"op":"init","config":{"features":[],"accounts":[{"pubkey":"SysvarC1ock11111111111111111111111111111111","account":{"data":["AA==","base64"],"owner":Pubkey::default().to_string(),"lamports":100}}],"programs":[],"payer":Pubkey::new_unique().to_string()}});
        assert!(session.request(&init)["error"].is_object());
        assert!(session.runner.is_none());
        init["config"]["accounts"] = json!([]);
        assert_eq!(session.request(&init)["result"]["ready"], true);
        assert_eq!(
            session
                .runner
                .as_ref()
                .unwrap()
                .svm
                .get_sysvar::<Clock>()
                .slot,
            0
        );
    }

    #[test]
    fn reset_overrides_commit_only_after_validation() {
        let mut session = Session::default();
        let key = Pubkey::new_unique();
        let state = |lamports| json!({"pubkey":key.to_string(),"account":{"data":["AA==","base64"],"owner":Pubkey::default().to_string(),"lamports":lamports}});
        let request = |op: &str| json!({"schema":1,"id":0,"op":op});
        let mut init = request("init");
        init["config"] = json!({"features":[],"accounts":[state(100)],"programs":[],"payer":Pubkey::new_unique().to_string()});
        assert_eq!(session.request(&init)["result"]["ready"], true);
        let runner = session.runner.as_mut().unwrap();
        runner
            .svm
            .set_account(key, account_from_json(&state(200)["account"]).unwrap())
            .unwrap();
        runner.statuses.insert("kept".into(), Value::Null);
        let mut bad = state(300);
        bad["pubkey"] = json!("SysvarC1ock11111111111111111111111111111111");
        let mut reset = request("reset");
        reset["accounts"] = json!([state(150), bad]);
        assert!(session.request(&reset)["error"].is_object());
        let runner = session.runner.as_ref().unwrap();
        assert_eq!(runner.svm.get_account(&key).unwrap().lamports, 200);
        assert!(runner.statuses.contains_key("kept"));
        reset["accounts"] = json!([state(150)]);
        assert_eq!(session.request(&reset)["result"]["done"], true);
        assert_eq!(
            session
                .runner
                .as_ref()
                .unwrap()
                .svm
                .get_account(&key)
                .unwrap()
                .lamports,
            150
        );
        assert!(session.runner.as_ref().unwrap().statuses.is_empty());
        assert_eq!(session.request(&request("reset"))["result"]["done"], true);
        assert_eq!(
            session
                .runner
                .as_ref()
                .unwrap()
                .svm
                .get_account(&key)
                .unwrap()
                .lamports,
            100
        );
    }
}
