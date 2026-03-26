//! FFI bindings for CGo integration

use libc::{c_char, c_int, size_t};
use std::ffi::{CStr, CString};
use std::ptr;
use std::slice;

use crate::keygen::generate_key_id;
use crate::sign::Claims;
use crate::verify::VerifyOptions;
use crate::{generate_ec_keypair, sign_jwt, to_jwk, verify_jwt};

/// Result structure for string operations
#[repr(C)]
pub struct JwtResult {
    pub error_code: c_int,
    pub result: *mut c_char,
}

/// Key pair result
#[repr(C)]
pub struct KeyPairResult {
    pub error_code: c_int,
    pub algorithm: *mut c_char,
    pub key_id: *mut c_char,
    pub private_key: *mut u8,
    pub private_key_len: size_t,
    pub public_key: *mut u8,
    pub public_key_len: size_t,
}

/// Claims result (after verification)
#[repr(C)]
pub struct ClaimsResult {
    pub error_code: c_int,
    /// JSON-encoded claims
    pub claims_json: *mut c_char,
    /// Key ID from header (nullable)
    pub key_id: *mut c_char,
}

// ============================================================================
// Key Generation
// ============================================================================

/// Generate an EC P-256 key pair for ES256 signing
///
/// # Safety
/// - `key_id` must be a valid null-terminated C string
/// - All output pointers in KeyPairResult must be freed with appropriate free functions
#[no_mangle]
pub unsafe extern "C" fn jwt_generate_ec_keypair(key_id: *const c_char) -> KeyPairResult {
    let key_id_str = if key_id.is_null() {
        generate_key_id()
    } else {
        match CStr::from_ptr(key_id).to_str() {
            Ok(s) => s.to_string(),
            Err(_) => return error_keypair_result(-1),
        }
    };

    match generate_ec_keypair(&key_id_str) {
        Ok(kp) => {
            let alg = CString::new(kp.algorithm).unwrap();
            let kid = CString::new(kp.key_id).unwrap();

            let priv_len = kp.private_key_der.len();
            let pub_len = kp.public_key_der.len();

            let priv_box = kp.private_key_der.into_boxed_slice();
            let pub_box = kp.public_key_der.into_boxed_slice();

            KeyPairResult {
                error_code: 0,
                algorithm: alg.into_raw(),
                key_id: kid.into_raw(),
                private_key: Box::into_raw(priv_box) as *mut u8,
                private_key_len: priv_len,
                public_key: Box::into_raw(pub_box) as *mut u8,
                public_key_len: pub_len,
            }
        }
        Err(e) => error_keypair_result(e.to_error_code()),
    }
}

fn error_keypair_result(code: c_int) -> KeyPairResult {
    KeyPairResult {
        error_code: code,
        algorithm: ptr::null_mut(),
        key_id: ptr::null_mut(),
        private_key: ptr::null_mut(),
        private_key_len: 0,
        public_key: ptr::null_mut(),
        public_key_len: 0,
    }
}

// ============================================================================
// JWT Signing
// ============================================================================

/// Sign a JWT with the given claims
///
/// # Safety
/// - `claims_json` must be valid JSON
/// - `algorithm` must be "ES256" or "EdDSA"
/// - `private_key` and `private_key_len` must be valid
/// - `key_id` can be null
#[no_mangle]
pub unsafe extern "C" fn jwt_sign(
    claims_json: *const c_char,
    algorithm: *const c_char,
    private_key: *const u8,
    private_key_len: size_t,
    key_id: *const c_char,
) -> JwtResult {
    if claims_json.is_null() || algorithm.is_null() || private_key.is_null() {
        return JwtResult {
            error_code: -1,
            result: ptr::null_mut(),
        };
    }

    let claims_str = match CStr::from_ptr(claims_json).to_str() {
        Ok(s) => s,
        Err(_) => {
            return JwtResult {
                error_code: -9,
                result: ptr::null_mut(),
            }
        }
    };

    let claims: Claims = match serde_json::from_str(claims_str) {
        Ok(c) => c,
        Err(_) => {
            return JwtResult {
                error_code: -9,
                result: ptr::null_mut(),
            }
        }
    };

    let alg_str = match CStr::from_ptr(algorithm).to_str() {
        Ok(s) => s,
        Err(_) => {
            return JwtResult {
                error_code: -5,
                result: ptr::null_mut(),
            }
        }
    };

    let key_slice = slice::from_raw_parts(private_key, private_key_len);

    let kid = if key_id.is_null() {
        None
    } else {
        CStr::from_ptr(key_id).to_str().ok()
    };

    match sign_jwt(&claims, alg_str, key_slice, kid) {
        Ok(token) => {
            let c_token = CString::new(token).unwrap();
            JwtResult {
                error_code: 0,
                result: c_token.into_raw(),
            }
        }
        Err(e) => JwtResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

// ============================================================================
// JWT Verification
// ============================================================================

/// Verify a JWT and return the claims
///
/// # Safety
/// - All string parameters must be valid null-terminated C strings
/// - `public_key` and `public_key_len` must be valid
#[no_mangle]
pub unsafe extern "C" fn jwt_verify(
    token: *const c_char,
    algorithm: *const c_char,
    public_key: *const u8,
    public_key_len: size_t,
    issuer: *const c_char,
    audience: *const c_char,
    leeway: u64,
) -> ClaimsResult {
    if token.is_null() || algorithm.is_null() || public_key.is_null() {
        return ClaimsResult {
            error_code: -1,
            claims_json: ptr::null_mut(),
            key_id: ptr::null_mut(),
        };
    }

    let token_str = match CStr::from_ptr(token).to_str() {
        Ok(s) => s,
        Err(_) => return error_claims_result(-4),
    };

    let alg_str = match CStr::from_ptr(algorithm).to_str() {
        Ok(s) => s,
        Err(_) => return error_claims_result(-5),
    };

    let key_slice = slice::from_raw_parts(public_key, public_key_len);

    let options = VerifyOptions {
        issuer: if issuer.is_null() {
            None
        } else {
            CStr::from_ptr(issuer).to_str().ok().map(|s| s.to_string())
        },
        audience: if audience.is_null() {
            None
        } else {
            CStr::from_ptr(audience)
                .to_str()
                .ok()
                .map(|s| s.to_string())
        },
        leeway,
        ignore_exp: false,
        ignore_nbf: false,
    };

    match verify_jwt(token_str, alg_str, key_slice, &options) {
        Ok(result) => {
            let claims_json = serde_json::to_string(&result.claims).unwrap();
            let c_claims = CString::new(claims_json).unwrap();

            let c_kid = result
                .header
                .kid
                .map(|k| CString::new(k).unwrap().into_raw())
                .unwrap_or(ptr::null_mut());

            ClaimsResult {
                error_code: 0,
                claims_json: c_claims.into_raw(),
                key_id: c_kid,
            }
        }
        Err(e) => error_claims_result(e.to_error_code()),
    }
}

fn error_claims_result(code: c_int) -> ClaimsResult {
    ClaimsResult {
        error_code: code,
        claims_json: ptr::null_mut(),
        key_id: ptr::null_mut(),
    }
}

// ============================================================================
// JWKS
// ============================================================================

/// Convert a public key to JWK JSON format
///
/// # Safety
/// - All parameters must be valid
#[no_mangle]
pub unsafe extern "C" fn jwt_to_jwk(
    algorithm: *const c_char,
    key_id: *const c_char,
    public_key: *const u8,
    public_key_len: size_t,
) -> JwtResult {
    if algorithm.is_null() || public_key.is_null() {
        return JwtResult {
            error_code: -1,
            result: ptr::null_mut(),
        };
    }

    let alg_str = match CStr::from_ptr(algorithm).to_str() {
        Ok(s) => s,
        Err(_) => {
            return JwtResult {
                error_code: -5,
                result: ptr::null_mut(),
            }
        }
    };

    let kid_str = if key_id.is_null() {
        generate_key_id()
    } else {
        match CStr::from_ptr(key_id).to_str() {
            Ok(s) => s.to_string(),
            Err(_) => {
                return JwtResult {
                    error_code: -1,
                    result: ptr::null_mut(),
                }
            }
        }
    };

    let pub_key = slice::from_raw_parts(public_key, public_key_len);

    // Create a minimal keypair just for JWK conversion
    let keypair = crate::KeyPair {
        algorithm: alg_str.to_string(),
        key_id: kid_str,
        private_key_der: vec![], // Not needed for JWK
        public_key_der: pub_key.to_vec(),
    };

    match to_jwk(&keypair) {
        Ok(jwk) => match serde_json::to_string(&jwk) {
            Ok(json) => {
                let c_json = CString::new(json).unwrap();
                JwtResult {
                    error_code: 0,
                    result: c_json.into_raw(),
                }
            }
            Err(_) => JwtResult {
                error_code: -9,
                result: ptr::null_mut(),
            },
        },
        Err(e) => JwtResult {
            error_code: e.to_error_code(),
            result: ptr::null_mut(),
        },
    }
}

// ============================================================================
// Memory Management
// ============================================================================

/// Free a string returned by JWT functions
#[no_mangle]
pub unsafe extern "C" fn jwt_free_string(s: *mut c_char) {
    if !s.is_null() {
        drop(CString::from_raw(s));
    }
}

/// Free bytes returned by JWT functions
#[no_mangle]
pub unsafe extern "C" fn jwt_free_bytes(data: *mut u8, len: size_t) {
    if !data.is_null() {
        drop(Vec::from_raw_parts(data, len, len));
    }
}

/// Free a KeyPairResult
#[no_mangle]
pub unsafe extern "C" fn jwt_free_keypair(result: KeyPairResult) {
    if !result.algorithm.is_null() {
        drop(CString::from_raw(result.algorithm));
    }
    if !result.key_id.is_null() {
        drop(CString::from_raw(result.key_id));
    }
    if !result.private_key.is_null() {
        drop(Vec::from_raw_parts(
            result.private_key,
            result.private_key_len,
            result.private_key_len,
        ));
    }
    if !result.public_key.is_null() {
        drop(Vec::from_raw_parts(
            result.public_key,
            result.public_key_len,
            result.public_key_len,
        ));
    }
}

/// Free a ClaimsResult
#[no_mangle]
pub unsafe extern "C" fn jwt_free_claims(result: ClaimsResult) {
    if !result.claims_json.is_null() {
        drop(CString::from_raw(result.claims_json));
    }
    if !result.key_id.is_null() {
        drop(CString::from_raw(result.key_id));
    }
}
