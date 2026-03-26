//! Aegion Crypto Library
//!
//! Security-critical cryptographic operations implemented in Rust.
//! Exposed to Go via CGo bindings.

mod error;
mod hash;
mod encrypt;
mod compare;
mod ffi;

pub use error::CryptoError;
pub use hash::{hash_password, verify_password};
pub use encrypt::{encrypt_field, decrypt_field};
pub use compare::constant_time_compare;
