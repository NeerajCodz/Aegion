//! Aegion Crypto Library
//!
//! Security-critical cryptographic operations implemented in Rust.
//! Exposed to Go via CGo bindings.

mod compare;
mod encrypt;
mod error;
mod ffi;
mod hash;
mod hmacsig;
mod identity;
mod internal_token;
mod opaque;
mod pkce;
mod session;
mod util;

pub use compare::{constant_time_compare, constant_time_compare_hmac, constant_time_compare_str};
pub use encrypt::{decrypt_field, encrypt_field};
pub use error::CryptoError;
pub use hash::{hash_password, needs_rehash, verify_password};
pub use hmacsig::{sign_envelope, sign_hex, verify_envelope, verify_hex};
pub use identity::{sign_identity_envelope, verify_identity_envelope};
pub use internal_token::{
    generate as generate_internal_token, verify as verify_internal_token, ParsedInternalToken,
};
pub use opaque::{hash_opaque_token, lookup_prefix, validate_opaque_token};
pub use pkce::{compute_challenge as compute_pkce_challenge, verify_pkce};
pub use session::{sign_cookie as sign_session_cookie, verify_cookie as verify_session_cookie};
