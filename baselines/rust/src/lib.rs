#![no_std]

extern "C" { fn abort() -> !; }
#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! { unsafe { abort() } }

unsafe fn load(p: *const u8) -> u64 {
    let mut n = 0u64;
    for i in 0..8 { n |= (*p.add(i) as u64) << (i * 8); }
    n
}
unsafe fn store(p: *mut u8, n: u64) {
    for i in 0..8 { *p.add(i) = (n >> (i * 8)) as u8; }
}

// Identical one-account ABI and checks to the Go experiment, with no SDK overhead.
#[no_mangle]
pub unsafe extern "C" fn entrypoint(input: *mut u8) -> u64 {
    if load(input) != 1 { return 10; }
    if *input.add(8) != 255 || *input.add(10) == 0 || *input.add(11) != 0 { return 11; }
    if load(input.add(88)) != 24 { return 12; }
    let tail = input.add(96 + 24 + 10240 + 8);
    if load(tail) != 16 { return 13; }
    for i in 0..32 { if *input.add(48+i) != *tail.add(24+i) { return 14; } }
    let state = core::slice::from_raw_parts_mut(input.add(96), 24);
    let instruction = core::slice::from_raw_parts(tail.add(8), 16);
    process(state, instruction)
}

fn process(state: &mut [u8], instruction: &[u8]) -> u64 {
    if state.len() != 24 || instruction.len() != 16 { return 1; }
    let x = unsafe { load(state.as_ptr()) };
    let y = unsafe { load(state.as_ptr().add(8)) };
    let n = unsafe { load(state.as_ptr().add(16)) };
    let amount = unsafe { load(instruction.as_ptr()) };
    let minimum = unsafe { load(instruction.as_ptr().add(8)) };
    if x == 0 || y == 0 || x > 1_000_000_000 || y > 1_000_000_000 { return 2; }
    if amount == 0 || amount > 1_000_000 || x + amount > 1_000_000_000 { return 3; }
    if n == u64::MAX { return 4; }
    let effective = amount * 9970;
    let out = y * effective / (x * 10000 + effective);
    if out == 0 || out < minimum { return 5; }
    unsafe {
        store(state.as_mut_ptr(), x + amount);
        store(state.as_mut_ptr().add(8), y - out);
        store(state.as_mut_ptr().add(16), n + 1);
    }
    0
}
