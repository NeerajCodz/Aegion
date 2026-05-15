//! FFI bindings for CGo integration.

use libc::{c_char, c_int, size_t};
use std::ffi::{CStr, CString};
use std::ptr;
use std::slice;

use crate::encrypt::generate_key;
use crate::{
    compute_pkce_challenge, constant_time_compare, decrypt_field, encrypt_field,
    generate_internal_token, hash_opaque_token, hash_password, lookup_prefix, sign_envelope,
    sign_hex, sign_session_cookie, validate_opaque_token, verify_envelope, verify_internal_token,
    verify_password, verify_pkce, verify_session_cookie,
};

#[repr(C)]
pub struct CryptoResult {
    pub error_code: c_int,
    pub result: *mut c_char,
}

#[repr(C)]
pub struct BytesResult {
    pub error_code: c_int,
    pub data: *mut u8,
    pub len: size_t,
}

#[repr(C)]
pub struct ParsedTokenResult {
    pub error_code: c_int,
    pub module_id: *mut c_char,
    pub timestamp: i64,
    pub signature_hex: *mut c_char,
}

#[no_mangle]
pub unsafe extern "C" fn crypto_hash_password(
    password: *const u8,
    password_len: size_t,
) -> CryptoResult {
    if password.is_null() && password_len > 0 {
        return CryptoResult {
            error_code: -1,
            result: ptr::null_mut(),
        };
    }

    let password_bytes = if password_len == 0 {
        &[]
    } else {
        slice::from_raw_parts(password, password_len)
    };
    let password_str = match std::str::from_utf8(password_bytes) {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -1,
                result: ptr::null_mut(),
            }
        }
    };

    match hash_password(password_str) {
        Ok(hash) => CryptoResult {
            error_code: 0,
            result: CString::new(hash).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_verify_password(
    password: *const u8,
    password_len: size_t,
    hash: *const c_char,
) -> c_int {
    if (password.is_null() && password_len > 0) || hash.is_null() {
        return -1;
    }

    let password_bytes = if password_len == 0 {
        &[]
    } else {
        slice::from_raw_parts(password, password_len)
    };
    let password_str = match std::str::from_utf8(password_bytes) {
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
        Ok(ciphertext) => CryptoResult {
            error_code: 0,
            result: CString::new(ciphertext).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

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
            let data = Box::into_raw(boxed) as *mut u8;
            BytesResult {
                error_code: 0,
                data,
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

#[no_mangle]
pub unsafe extern "C" fn crypto_opaque_hash(token: *const c_char) -> CryptoResult {
    if token.is_null() {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let token_str = match CStr::from_ptr(token).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -8,
                result: ptr::null_mut(),
            }
        }
    };

    CryptoResult {
        error_code: 0,
        result: CString::new(hash_opaque_token(token_str))
            .unwrap()
            .into_raw(),
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_opaque_validate(
    token: *const c_char,
    expected_hash: *const c_char,
) -> c_int {
    if token.is_null() || expected_hash.is_null() {
        return 0;
    }

    let token_str = match CStr::from_ptr(token).to_str() {
        Ok(s) => s,
        Err(_) => return 0,
    };
    let hash_str = match CStr::from_ptr(expected_hash).to_str() {
        Ok(s) => s,
        Err(_) => return 0,
    };

    if validate_opaque_token(token_str, hash_str) {
        1
    } else {
        0
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_opaque_prefix(
    token: *const c_char,
    length: size_t,
) -> CryptoResult {
    if token.is_null() {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let token_str = match CStr::from_ptr(token).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -8,
                result: ptr::null_mut(),
            }
        }
    };

    CryptoResult {
        error_code: 0,
        result: CString::new(lookup_prefix(token_str, length))
            .unwrap()
            .into_raw(),
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_hmac_sha256_hex(
    secret: *const u8,
    secret_len: size_t,
    message: *const u8,
    message_len: size_t,
) -> CryptoResult {
    if secret.is_null() || (message.is_null() && message_len > 0) {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let secret_slice = slice::from_raw_parts(secret, secret_len);
    let message_slice = if message_len == 0 {
        &[]
    } else {
        slice::from_raw_parts(message, message_len)
    };

    match sign_hex(secret_slice, message_slice) {
        Ok(signature) => CryptoResult {
            error_code: 0,
            result: CString::new(signature).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_sign_envelope(
    kind: *const c_char,
    secret: *const u8,
    secret_len: size_t,
    timestamp: i64,
    payload: *const u8,
    payload_len: size_t,
) -> CryptoResult {
    if kind.is_null() || secret.is_null() || (payload.is_null() && payload_len > 0) {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let kind_str = match CStr::from_ptr(kind).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -8,
                result: ptr::null_mut(),
            }
        }
    };
    let secret_slice = slice::from_raw_parts(secret, secret_len);
    let payload_slice = if payload_len == 0 {
        &[]
    } else {
        slice::from_raw_parts(payload, payload_len)
    };

    match sign_envelope(kind_str, secret_slice, timestamp, payload_slice) {
        Ok(envelope) => CryptoResult {
            error_code: 0,
            result: CString::new(envelope).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_verify_envelope(
    kind: *const c_char,
    secret: *const u8,
    secret_len: size_t,
    payload: *const u8,
    payload_len: size_t,
    envelope: *const c_char,
    max_age_seconds: u64,
    now_unix: i64,
) -> c_int {
    if kind.is_null()
        || secret.is_null()
        || envelope.is_null()
        || (payload.is_null() && payload_len > 0)
    {
        return 0;
    }

    let kind_str = match CStr::from_ptr(kind).to_str() {
        Ok(s) => s,
        Err(_) => return 0,
    };
    let secret_slice = slice::from_raw_parts(secret, secret_len);
    let payload_slice = if payload_len == 0 {
        &[]
    } else {
        slice::from_raw_parts(payload, payload_len)
    };
    let envelope_str = match CStr::from_ptr(envelope).to_str() {
        Ok(s) => s,
        Err(_) => return 0,
    };

    match verify_envelope(
        kind_str,
        secret_slice,
        payload_slice,
        envelope_str,
        max_age_seconds,
        now_unix,
    ) {
        Ok(true) => 1,
        _ => 0,
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_sign_session_cookie(
    secret: *const u8,
    secret_len: size_t,
    token: *const c_char,
    timestamp: i64,
) -> CryptoResult {
    if secret.is_null() || token.is_null() {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let secret_slice = slice::from_raw_parts(secret, secret_len);
    let token_str = match CStr::from_ptr(token).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -8,
                result: ptr::null_mut(),
            }
        }
    };

    match sign_session_cookie(secret_slice, token_str, timestamp) {
        Ok(value) => CryptoResult {
            error_code: 0,
            result: CString::new(value).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_verify_session_cookie(
    secret: *const u8,
    secret_len: size_t,
    signed: *const c_char,
    max_age_seconds: u64,
    now_unix: i64,
) -> CryptoResult {
    if secret.is_null() || signed.is_null() {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let secret_slice = slice::from_raw_parts(secret, secret_len);
    let signed_str = match CStr::from_ptr(signed).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -8,
                result: ptr::null_mut(),
            }
        }
    };

    match verify_session_cookie(secret_slice, signed_str, now_unix, max_age_seconds) {
        Ok(token) => CryptoResult {
            error_code: 0,
            result: CString::new(token).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_generate_internal_token(
    secret: *const u8,
    secret_len: size_t,
    module_id: *const c_char,
    timestamp: i64,
) -> CryptoResult {
    if secret.is_null() || module_id.is_null() {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let secret_slice = slice::from_raw_parts(secret, secret_len);
    let module_str = match CStr::from_ptr(module_id).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -8,
                result: ptr::null_mut(),
            }
        }
    };

    match generate_internal_token(secret_slice, module_str, timestamp) {
        Ok(token) => CryptoResult {
            error_code: 0,
            result: CString::new(token).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_verify_internal_token(
    secret: *const u8,
    secret_len: size_t,
    token: *const c_char,
    ttl_seconds: u64,
    now_unix: i64,
) -> ParsedTokenResult {
    if secret.is_null() || token.is_null() {
        return ParsedTokenResult {
            error_code: -8,
            module_id: ptr::null_mut(),
            timestamp: 0,
            signature_hex: ptr::null_mut(),
        };
    }

    let secret_slice = slice::from_raw_parts(secret, secret_len);
    let token_str = match CStr::from_ptr(token).to_str() {
        Ok(s) => s,
        Err(_) => {
            return ParsedTokenResult {
                error_code: -8,
                module_id: ptr::null_mut(),
                timestamp: 0,
                signature_hex: ptr::null_mut(),
            }
        }
    };

    match verify_internal_token(secret_slice, token_str, now_unix, ttl_seconds) {
        Ok(parsed) => ParsedTokenResult {
            error_code: 0,
            module_id: CString::new(parsed.module_id).unwrap().into_raw(),
            timestamp: parsed.timestamp_millis,
            signature_hex: CString::new(parsed.signature_hex).unwrap().into_raw(),
        },
        Err(e) => ParsedTokenResult {
            error_code: e.to_error_code(),
            module_id: ptr::null_mut(),
            timestamp: 0,
            signature_hex: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_pkce_challenge(
    verifier: *const c_char,
    method: *const c_char,
) -> CryptoResult {
    if verifier.is_null() {
        return CryptoResult {
            error_code: -8,
            result: ptr::null_mut(),
        };
    }

    let verifier_str = match CStr::from_ptr(verifier).to_str() {
        Ok(s) => s,
        Err(_) => {
            return CryptoResult {
                error_code: -8,
                result: ptr::null_mut(),
            }
        }
    };
    let method_str = if method.is_null() {
        ""
    } else {
        CStr::from_ptr(method).to_str().unwrap_or("")
    };

    match compute_pkce_challenge(verifier_str, method_str) {
        Ok(challenge) => CryptoResult {
            error_code: 0,
            result: CString::new(challenge).unwrap().into_raw(),
        },
        Err(e) => CryptoResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_pkce_verify(
    verifier: *const c_char,
    challenge: *const c_char,
    method: *const c_char,
) -> c_int {
    if verifier.is_null() || challenge.is_null() {
        return -1;
    }

    let verifier_str = match CStr::from_ptr(verifier).to_str() {
        Ok(s) => s,
        Err(_) => return -1,
    };
    let challenge_str = match CStr::from_ptr(challenge).to_str() {
        Ok(s) => s,
        Err(_) => return -1,
    };
    let method_str = if method.is_null() {
        ""
    } else {
        CStr::from_ptr(method).to_str().unwrap_or("")
    };

    match verify_pkce(verifier_str, challenge_str, method_str) {
        Ok(true) => 1,
        Ok(false) => 0,
        Err(e) => e.to_error_code(),
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_free_string(s: *mut c_char) {
    if !s.is_null() {
        drop(CString::from_raw(s));
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_free_bytes(data: *mut u8, len: size_t) {
    if !data.is_null() {
        drop(Vec::from_raw_parts(data, len, len));
    }
}

#[no_mangle]
pub unsafe extern "C" fn crypto_free_parsed_token(result: ParsedTokenResult) {
    if !result.module_id.is_null() {
        drop(CString::from_raw(result.module_id));
    }
    if !result.signature_hex.is_null() {
        drop(CString::from_raw(result.signature_hex));
    }
}
