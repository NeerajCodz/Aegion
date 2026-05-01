package crypto

import (
	"encoding/hex"
	"time"
)

// ValidatedInternalToken is a parsed, verified internal token.
type ValidatedInternalToken struct {
	ModuleID  string
	Timestamp time.Time
	Signature []byte
}

// GenerateInternalToken creates a versioned internal module token.
func GenerateInternalToken(secret []byte, moduleID string, now time.Time) (string, error) {
	return cGenerateInternalToken(secret, moduleID, now.UTC().UnixMilli())
}

// VerifyInternalToken validates a versioned internal token using a single secret.
func VerifyInternalToken(secret []byte, token string, ttl time.Duration, now time.Time) (*ValidatedInternalToken, error) {
	ttlMillis := uint64(0)
	if ttl > 0 {
		ttlMillis = uint64(ttl / time.Millisecond)
		if ttlMillis == 0 {
			ttlMillis = 1
		}
	}

	parsed, err := cVerifyInternalToken(secret, token, ttlMillis, now.UTC().UnixMilli())
	if err != nil {
		return nil, err
	}

	signature, err := hex.DecodeString(parsed.SignatureHex)
	if err != nil {
		return nil, ErrInternalToken
	}

	return &ValidatedInternalToken{
		ModuleID:  parsed.ModuleID,
		Timestamp: time.UnixMilli(parsed.TimestampUnix).UTC(),
		Signature: signature,
	}, nil
}
