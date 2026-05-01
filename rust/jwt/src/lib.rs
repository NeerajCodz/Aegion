//! Aegion JWT Library
//!
//! JWT signing, verification, and JWKS management.
//! Supports ES256 and EdDSA algorithms.

mod error;
mod ffi;
mod jwks;
mod keygen;
mod sign;
mod verify;

pub use error::JwtError;
pub use jwks::{jwks_to_json, to_jwk, to_jwks, Jwk, Jwks};
pub use keygen::{
    generate_ec_keypair, generate_ed25519_keypair, generate_key_id, generate_rsa_keypair, KeyPair,
};
pub use sign::sign_jwt;
pub use verify::verify_jwt;
