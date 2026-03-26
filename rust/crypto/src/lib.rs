//! Aegion Crypto Library
//!
//! Security-critical cryptographic operations implemented in Rust.
//! Exposed to Go via CGo bindings.

mod compare;
mod encrypt;
mod error;
mod ffi;
mod hash;

pub use compare::constant_time_compare;
pub use encrypt::{decrypt_field, encrypt_field};
pub use error::CryptoError;
pub use hash::{hash_password, verify_password};
