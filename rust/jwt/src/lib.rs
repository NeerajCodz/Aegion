//! Aegion JWT Library
//!
//! JWT signing, verification, and JWKS management.
//! Supports RS256 and ES256 algorithms.

mod error;
mod keygen;
mod sign;
mod verify;
mod jwks;
mod ffi;

pub use error::JwtError;
pub use keygen::{generate_rsa_keypair, generate_ec_keypair, KeyPair};
pub use sign::sign_jwt;
pub use verify::verify_jwt;
pub use jwks::{to_jwk, to_jwks, Jwk, Jwks};
