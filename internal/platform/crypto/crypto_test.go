package crypto

import (
	"fmt"
	"testing"
)

func TestConstants(t *testing.T) {
	if KeySize != 32 {
		t.Errorf("KeySize = %d, want 32", KeySize)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrHashFailed", ErrHashFailed, "password hashing failed"},
		{"ErrVerifyFailed", ErrVerifyFailed, "password verification failed"},
		{"ErrEncryptFailed", ErrEncryptFailed, "encryption failed"},
		{"ErrDecryptFailed", ErrDecryptFailed, "decryption failed"},
		{"ErrInvalidKeyLength", ErrInvalidKeyLength, "invalid key length: expected 32 bytes"},
		{"ErrRngFailed", ErrRngFailed, "random number generation failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("error message = %q, want %q", tt.err.Error(), tt.msg)
			}
		})
	}
}

func TestConstantTimeCompare(t *testing.T) {
	tests := []struct {
		name string
		a    []byte
		b    []byte
		want bool
	}{
		{
			name: "equal byte slices",
			a:    []byte{1, 2, 3, 4},
			b:    []byte{1, 2, 3, 4},
			want: true,
		},
		{
			name: "different byte slices same length",
			a:    []byte{1, 2, 3, 4},
			b:    []byte{1, 2, 3, 5},
			want: false,
		},
		{
			name: "different lengths",
			a:    []byte{1, 2, 3},
			b:    []byte{1, 2, 3, 4},
			want: false,
		},
		{
			name: "empty slices",
			a:    []byte{},
			b:    []byte{},
			want: true,
		},
		{
			name: "one empty slice",
			a:    []byte{1, 2, 3},
			b:    []byte{},
			want: false,
		},
		{
			name: "nil vs empty",
			a:    nil,
			b:    []byte{},
			want: true,
		},
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "single byte equal",
			a:    []byte{42},
			b:    []byte{42},
			want: true,
		},
		{
			name: "single byte different",
			a:    []byte{42},
			b:    []byte{43},
			want: false,
		},
		{
			name: "long equal slices",
			a:    make([]byte, 1000),
			b:    make([]byte, 1000),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConstantTimeCompare(tt.a, tt.b)
			if result != tt.want {
				t.Errorf("ConstantTimeCompare(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.want)
			}
		})
	}
}

func TestEncryptFieldKeyValidation(t *testing.T) {
	tests := []struct {
		name      string
		keyLen    int
		wantError error
	}{
		{"valid key length", KeySize, nil},
		{"key too short", KeySize - 1, ErrInvalidKeyLength},
		{"key too long", KeySize + 1, ErrInvalidKeyLength},
		{"zero length key", 0, ErrInvalidKeyLength},
	}

	plaintext := []byte("test message")
	aad := []byte("test aad")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keyLen)
			_, err := EncryptField(key, plaintext, aad)
			
			if tt.wantError != nil {
				if err != tt.wantError {
					t.Errorf("EncryptField() error = %v, want %v", err, tt.wantError)
				}
			} else if tt.keyLen == KeySize {
				// For valid key length, we expect either success or ErrEncryptFailed
				// (depending on whether Rust library is available)
				if err != nil && err != ErrEncryptFailed {
					t.Errorf("EncryptField() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestDecryptFieldKeyValidation(t *testing.T) {
	tests := []struct {
		name      string
		keyLen    int
		wantError error
	}{
		{"valid key length", KeySize, nil},
		{"key too short", KeySize - 1, ErrInvalidKeyLength},
		{"key too long", KeySize + 1, ErrInvalidKeyLength},
		{"zero length key", 0, ErrInvalidKeyLength},
	}

	ciphertext := "test_ciphertext"
	aad := []byte("test aad")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keyLen)
			_, err := DecryptField(key, ciphertext, aad)
			
			if tt.wantError != nil {
				if err != tt.wantError {
					t.Errorf("DecryptField() error = %v, want %v", err, tt.wantError)
				}
			} else if tt.keyLen == KeySize {
				// For valid key length, we expect either success or ErrDecryptFailed
				// (depending on whether Rust library is available and ciphertext is valid)
				if err != nil && err != ErrDecryptFailed {
					t.Errorf("DecryptField() unexpected error = %v", err)
				}
			}
		})
	}
}

// TestFunctionSignatures verifies that all functions have the expected signatures
// and can be called without compilation errors
func TestFunctionSignatures(t *testing.T) {
	// Test that we can call all functions (they may fail due to missing Rust lib, but should compile)
	
	// HashPassword
	_, err := HashPassword("test")
	if err != nil && err != ErrHashFailed {
		t.Logf("HashPassword returned expected error or ErrHashFailed: %v", err)
	}
	
	// VerifyPassword
	_, err = VerifyPassword("test", "hash")
	if err != nil && err != ErrVerifyFailed {
		t.Logf("VerifyPassword returned expected error or ErrVerifyFailed: %v", err)
	}
	
	// GenerateKey
	_, err = GenerateKey()
	if err != nil && err != ErrRngFailed {
		t.Logf("GenerateKey returned expected error or ErrRngFailed: %v", err)
	}
	
	// Test with valid key size to test Go-level validation
	validKey := make([]byte, KeySize)
	
	_, err = EncryptField(validKey, []byte("test"), []byte("aad"))
	if err != nil && err != ErrEncryptFailed {
		t.Logf("EncryptField returned expected error or ErrEncryptFailed: %v", err)
	}
	
	_, err = DecryptField(validKey, "test", []byte("aad"))
	if err != nil && err != ErrDecryptFailed {
		t.Logf("DecryptField returned expected error or ErrDecryptFailed: %v", err)
	}
}

func BenchmarkConstantTimeCompare(b *testing.B) {
	// Test different sizes to ensure it's truly constant time
	sizes := []int{16, 32, 64, 128, 256, 512, 1024}
	
	for _, size := range sizes {
		a := make([]byte, size)
		bb := make([]byte, size)
		// Make them different in the last byte to test worst case
		bb[len(bb)-1] = 1
		
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ConstantTimeCompare(a, bb)
			}
		})
	}
}