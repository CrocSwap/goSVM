#![no_std]

/// Malformed canonical input (ABI status 4).
pub struct Malformed;

/// Constant-product quote: amount * reserve_out / (reserve_in + amount).
/// Input: amount u64 LE, reserve_in u32 LE, reserve_out u32 LE (16 bytes).
/// Output: quoted amount u64 LE (8 bytes). Zero reserves are malformed.
pub fn quote(input: &[u8; 16]) -> Result<[u8; 8], Malformed> {
    let [a0, a1, a2, a3, a4, a5, a6, a7, i0, i1, i2, i3, o0, o1, o2, o3] = *input;
    let amount = u64::from_le_bytes([a0, a1, a2, a3, a4, a5, a6, a7]) as u128;
    let reserve_in = u32::from_le_bytes([i0, i1, i2, i3]) as u128;
    let reserve_out = u32::from_le_bytes([o0, o1, o2, o3]) as u128;
    if reserve_in == 0 || reserve_out == 0 {
        return Err(Malformed);
    }
    // u128 keeps the product exact; the denominator is nonzero.
    let out = amount * reserve_out / (reserve_in + amount);
    Ok((out as u64).to_le_bytes())
}
