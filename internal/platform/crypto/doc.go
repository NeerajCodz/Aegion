// Package crypto provides Go wrappers around the Rust security primitives.
//
// Rust owns the cryptographic implementations for opaque token hashing,
// HMAC-based envelopes, session cookies, internal module tokens, identity
// header signatures, and PKCE helpers. Go packages should use this package for
// cryptographic operations and keep application logic limited to orchestration,
// transport, and persistence concerns.
package crypto
