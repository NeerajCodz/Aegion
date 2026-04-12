use ring::digest::{digest, SHA256};

use crate::util::b64_std_encode;

pub fn hash_opaque_token(token: &str) -> String {
    let digest = digest(&SHA256, token.as_bytes());
    b64_std_encode(digest.as_ref())
}

pub fn validate_opaque_token(token: &str, expected_hash: &str) -> bool {
    crate::compare::constant_time_compare_str(&hash_opaque_token(token), expected_hash)
}

pub fn lookup_prefix(token: &str, length: usize) -> String {
    if length == 0 {
        return String::new();
    }

    if token.len() <= length {
        token.to_string()
    } else {
        token[..length].to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hash_is_stable() {
        let token = "opaque-token";
        assert_eq!(hash_opaque_token(token), hash_opaque_token(token));
    }

    #[test]
    fn validate_hash() {
        let token = "opaque-token";
        let hash = hash_opaque_token(token);
        assert!(validate_opaque_token(token, &hash));
        assert!(!validate_opaque_token("other", &hash));
    }

    #[test]
    fn prefix_respects_bounds() {
        assert_eq!(lookup_prefix("abcdef", 0), "");
        assert_eq!(lookup_prefix("abcdef", 3), "abc");
        assert_eq!(lookup_prefix("abcdef", 10), "abcdef");
    }
}
