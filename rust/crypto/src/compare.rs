//! Constant-time comparison utilities
//!
//! Uses the `subtle` crate to prevent timing attacks.

use subtle::ConstantTimeEq;

/// Compare two byte slices in constant time
///
/// Returns true if the slices are equal, false otherwise.
/// The comparison time does not depend on the content of the slices,
/// only on their lengths.
///
/// # Security
/// This function is designed to prevent timing attacks by ensuring
/// the comparison takes the same amount of time regardless of how
/// many bytes match.
pub fn constant_time_compare(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    a.ct_eq(b).into()
}

/// Compare two strings in constant time
///
/// Wrapper around `constant_time_compare` for string comparison.
pub fn constant_time_compare_str(a: &str, b: &str) -> bool {
    constant_time_compare(a.as_bytes(), b.as_bytes())
}

/// Constant-time HMAC comparison
///
/// Specifically for comparing HMAC digests where timing attacks
/// could reveal information about the valid HMAC.
pub fn constant_time_compare_hmac(computed: &[u8], provided: &[u8]) -> bool {
    if computed.len() != provided.len() {
        // Still do a comparison to avoid timing differences
        // between length-mismatch and content-mismatch cases
        let dummy = vec![0u8; computed.len()];
        let _ = computed.ct_eq(&dummy);
        return false;
    }
    computed.ct_eq(provided).into()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_equal_bytes() {
        let a = b"hello world";
        let b = b"hello world";
        assert!(constant_time_compare(a, b));
    }

    #[test]
    fn test_unequal_bytes() {
        let a = b"hello world";
        let b = b"hello worle";
        assert!(!constant_time_compare(a, b));
    }

    #[test]
    fn test_different_lengths() {
        let a = b"short";
        let b = b"much longer string";
        assert!(!constant_time_compare(a, b));
    }

    #[test]
    fn test_empty() {
        assert!(constant_time_compare(b"", b""));
        assert!(!constant_time_compare(b"", b"a"));
    }

    #[test]
    fn test_string_comparison() {
        assert!(constant_time_compare_str("secret", "secret"));
        assert!(!constant_time_compare_str("secret", "secre"));
        assert!(!constant_time_compare_str("secret", "Secret"));
    }

    #[test]
    fn test_hmac_comparison() {
        let hmac1 = [0x01, 0x02, 0x03, 0x04];
        let hmac2 = [0x01, 0x02, 0x03, 0x04];
        let hmac3 = [0x01, 0x02, 0x03, 0x05];

        assert!(constant_time_compare_hmac(&hmac1, &hmac2));
        assert!(!constant_time_compare_hmac(&hmac1, &hmac3));
    }

    #[test]
    fn test_constant_time_compare_equal() {
        let data1 = b"exactly the same content";
        let data2 = b"exactly the same content";
        assert!(constant_time_compare(data1, data2));
    }

    #[test]
    fn test_constant_time_compare_different() {
        let data1 = b"this is different";
        let data2 = b"this is differnet"; // typo on purpose
        assert!(!constant_time_compare(data1, data2));
    }

    #[test]
    fn test_constant_time_compare_different_lengths() {
        let short = b"short";
        let long = b"this is much longer";
        assert!(!constant_time_compare(short, long));
        assert!(!constant_time_compare(long, short));
    }

    #[test]
    fn test_constant_time_compare_empty() {
        let empty1 = b"";
        let empty2 = b"";
        let non_empty = b"not empty";
        
        assert!(constant_time_compare(empty1, empty2));
        assert!(!constant_time_compare(empty1, non_empty));
        assert!(!constant_time_compare(non_empty, empty1));
    }

    #[test]
    fn test_constant_time_compare_str_equal() {
        let str1 = "hello world";
        let str2 = "hello world";
        assert!(constant_time_compare_str(str1, str2));

        // Test with Unicode
        let unicode1 = "café";
        let unicode2 = "café";
        assert!(constant_time_compare_str(unicode1, unicode2));
    }

    #[test]
    fn test_constant_time_compare_hmac_edge_cases() {
        // Test with empty slices
        let empty = &[];
        assert!(constant_time_compare_hmac(empty, empty));
        
        // Test with different lengths (triggers dummy comparison)
        let short = &[0x01, 0x02];
        let long = &[0x01, 0x02, 0x03, 0x04, 0x05];
        assert!(!constant_time_compare_hmac(short, long));
        assert!(!constant_time_compare_hmac(long, short));
        
        // Test real HMAC-sized data (32 bytes)
        let hmac1 = [0xFF; 32];
        let hmac2 = [0xFF; 32];
        let mut hmac3 = [0xFF; 32];
        hmac3[31] = 0xFE; // Change last byte
        
        assert!(constant_time_compare_hmac(&hmac1, &hmac2));
        assert!(!constant_time_compare_hmac(&hmac1, &hmac3));
    }
}
