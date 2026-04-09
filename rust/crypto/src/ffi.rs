//! FFI bindings for CGo integration
//!
//! These functions are exposed as C-compatible functions that can be
//! called from Go through CGo.

use libc::{c_char, c_int, size_t};
use std::ffi::{CStr, CString};
use std::ptr;
use std::slice;

use crate::encrypt::generate_key;
use crate::{constant_time_compare, decrypt_field, encrypt_field, hash_password, verify_password};

/// Result structure for functions that return strings
#[repr(C)]
pub struct CryptoResult {
    /// 0 on success, negative error code on failure
    pub error_code: c_int,
    /// Null-terminated result string (caller must free with crypto_free_string)
    pub result: *mut c_char,
}

/// Result structure for functions that return bytes
#[repr(C)]
pub struct BytesResult {
    /// 0 on success, negative error code on failure
    pub error_code: c_int,
    /// Result bytes (caller must free with crypto_free_bytes)
    pub data: *mut u8,
    /// Length of the data
    pub len: size_t,
}

// ============================================================================
// Password Hashing
// ============================================================================

/// Hash a password using Argon2id
///
/// # Safety
/// - `password` must be a valid null-terminated C string
/// - The returned `CryptoResult.result` must be freed with `crypto_free_string`
#[no_mangle]
pub unsafe extern "C" fn crypto_hash_password(password: *const c_char) -> CryptoResult {
    if password.is_null() {
        return CryptoResult {
            error_code: -1,
            result: ptr::null_mut(),
        };
    }

    let password_str = match CStr::from_ptr(password).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -1,
                result: ptr::null_mut(),
            }
        }
    };

    match hash_password(password_str) {
        Ok(hash) => {
            let c_hash = CString::new(hash).unwrap();
            CryptoResult {
                error_code: 0,
                result: c_hash.into_raw(),
            }
        }
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

/// Verify a password against an Argon2id hash
///
/// # Safety
/// - `password` must be a valid null-terminated C string
/// - `hash` must be a valid null-terminated C string
///
/// # Returns
/// - 1 if password matches
/// - 0 if password does not match
/// - Negative error code on failure
#[no_mangle]
pub unsafe extern "C" fn crypto_verify_password(
    password: *const c_char,
    hash: *const c_char,
) -> c_int {
    if password.is_null() || hash.is_null() {
        return -1;
    }

    let password_str = match CStr::from_ptr(password).to_str() {
        Ok(s) => s,
        Err(_) => return -1,
    };

    let hash_str = match CStr::from_ptr(hash).to_str() {
        Ok(s) => s,
        Err(_) => return -1,
    };

    match verify_password(password_str, hash_str) {
        Ok(true) => 1,
        Ok(false) => 0,
        Err(e) => e.to_error_code(),
    }
}

// ============================================================================
// Field Encryption
// ============================================================================

/// Encrypt a field with XChaCha20-Poly1305
///
/// # Safety
/// - `key` must point to exactly 32 bytes
/// - `plaintext` and `plaintext_len` must be valid
/// - `aad` can be null if `aad_len` is 0
/// - The returned `CryptoResult.result` must be freed with `crypto_free_string`
#[no_mangle]
pub unsafe extern "C" fn crypto_encrypt_field(
    key: *const u8,
    plaintext: *const u8,
    plaintext_len: size_t,
    aad: *const u8,
    aad_len: size_t,
) -> CryptoResult {
    if key.is_null() || (plaintext.is_null() && plaintext_len > 0) || (aad.is_null() && aad_len > 0)
    {
        return CryptoResult {
            error_code: -1,
            result: ptr::null_mut(),
        };
    }

    let key_slice = slice::from_raw_parts(key, 32);
    let plaintext_slice = if plaintext_len == 0 {
        &[]
    } else {
        slice::from_raw_parts(plaintext, plaintext_len)
    };
    let aad_opt = if aad_len == 0 {
        None
    } else {
        Some(slice::from_raw_parts(aad, aad_len))
    };

    match encrypt_field(key_slice, plaintext_slice, aad_opt) {
        Ok(ciphertext) => {
            let c_str = CString::new(ciphertext).unwrap();
            CryptoResult {
                error_code: 0,
                result: c_str.into_raw(),
            }
        }
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

/// Decrypt a field with XChaCha20-Poly1305
///
/// # Safety
/// - `key` must point to exactly 32 bytes
/// - `ciphertext` must be a valid null-terminated C string (base64)
/// - `aad` can be null if `aad_len` is 0
/// - The returned `BytesResult.data` must be freed with `crypto_free_bytes`
#[no_mangle]
pub unsafe extern "C" fn crypto_decrypt_field(
    key: *const u8,
    ciphertext: *const c_char,
    aad: *const u8,
    aad_len: size_t,
) -> BytesResult {
    if key.is_null() || ciphertext.is_null() || (aad.is_null() && aad_len > 0) {
        return BytesResult {
            error_code: -1,
            data: ptr::null_mut(),
            len: 0,
        };
    }

    let key_slice = slice::from_raw_parts(key, 32);
    let ciphertext_str = match CStr::from_ptr(ciphertext).to_str() {
        Ok(s) => s,
        Err(_) => {
            return BytesResult {
                error_code: -1,
                data: ptr::null_mut(),
                len: 0,
            }
        }
    };
    let aad_opt = if aad_len == 0 {
        None
    } else {
        Some(slice::from_raw_parts(aad, aad_len))
    };

    match decrypt_field(key_slice, ciphertext_str, aad_opt) {
        Ok(plaintext) => {
            let len = plaintext.len();
            let boxed = plaintext.into_boxed_slice();
            let ptr = Box::into_raw(boxed) as *mut u8;
            BytesResult {
                error_code: 0,
                data: ptr,
                len,
            }
        }
        Err(e) => BytesResult {
            error_code: e.to_error_code(),
            data: ptr::null_mut(),
            len: 0,
        },
    }
}

/// Generate a random 32-byte encryption key
///
/// # Safety
/// - `out` must point to a buffer of at least 32 bytes
///
/// # Returns
/// - 0 on success
/// - Negative error code on failure
#[no_mangle]
pub unsafe extern "C" fn crypto_generate_key(out: *mut u8) -> c_int {
    if out.is_null() {
        return -1;
    }

    match generate_key() {
        Ok(key) => {
            ptr::copy_nonoverlapping(key.as_ptr(), out, 32);
            0
        }
        Err(e) => e.to_error_code(),
    }
}

// ============================================================================
// Comparison
// ============================================================================

/// Constant-time byte comparison
///
/// # Safety
/// - `a` and `b` must point to valid memory of at least `len` bytes
///
/// # Returns
/// - 1 if equal
/// - 0 if not equal
#[no_mangle]
pub unsafe extern "C" fn crypto_constant_time_compare(
    a: *const u8,
    b: *const u8,
    len: size_t,
) -> c_int {
    if a.is_null() || b.is_null() {
        return 0;
    }

    let a_slice = slice::from_raw_parts(a, len);
    let b_slice = slice::from_raw_parts(b, len);

    if constant_time_compare(a_slice, b_slice) {
        1
    } else {
        0
    }
}

// ============================================================================
// Memory Management
// ============================================================================

/// Free a string returned by crypto functions
///
/// # Safety
/// - `s` must be a string returned by a crypto function, or null
#[no_mangle]
pub unsafe extern "C" fn crypto_free_string(s: *mut c_char) {
    if !s.is_null() {
        drop(CString::from_raw(s));
    }
}

/// Free bytes returned by crypto functions
///
/// # Safety
/// - `data` must be bytes returned by a crypto function, or null
/// - `len` must match the original length
#[no_mangle]
pub unsafe extern "C" fn crypto_free_bytes(data: *mut u8, len: size_t) {
    if !data.is_null() {
        drop(Vec::from_raw_parts(data, len, len));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ptr;

    #[test]
    fn encrypt_rejects_null_plaintext_with_nonzero_length() {
        let key = generate_key().unwrap();

        let result = unsafe { crypto_encrypt_field(key.as_ptr(), ptr::null(), 1, ptr::null(), 0) };

        assert_eq!(result.error_code, -1);
        assert!(result.result.is_null());
    }

    #[test]
    fn encrypt_and_decrypt_empty_plaintext() {
        let key = generate_key().unwrap();

        let encrypted =
            unsafe { crypto_encrypt_field(key.as_ptr(), ptr::null(), 0, ptr::null(), 0) };
        assert_eq!(encrypted.error_code, 0);
        assert!(!encrypted.result.is_null());

        let decrypted = unsafe {
            crypto_decrypt_field(
                key.as_ptr(),
                encrypted.result as *const c_char,
                ptr::null(),
                0,
            )
        };
        assert_eq!(decrypted.error_code, 0);
        assert_eq!(decrypted.len, 0);

        unsafe {
            crypto_free_string(encrypted.result);
            crypto_free_bytes(decrypted.data, decrypted.len);
        }
    }

    #[test]
    fn decrypt_rejects_null_aad_with_nonzero_length() {
        let key = generate_key().unwrap();
        let encrypted =
            unsafe { crypto_encrypt_field(key.as_ptr(), b"x".as_ptr(), 1, ptr::null(), 0) };
        assert_eq!(encrypted.error_code, 0);

        let result = unsafe {
            crypto_decrypt_field(
                key.as_ptr(),
                encrypted.result as *const c_char,
                ptr::null(),
                1,
            )
        };
        assert_eq!(result.error_code, -1);
        assert!(result.data.is_null());
        assert_eq!(result.len, 0);

        unsafe {
            crypto_free_string(encrypted.result);
        }
    }
}
