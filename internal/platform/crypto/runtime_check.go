package crypto

import "errors"

var runtimeSelfCheckCompare = ConstantTimeCompare

// ErrRuntimeSelfCheck indicates the Rust crypto runtime self-check failed.
var ErrRuntimeSelfCheck = errors.New("rust crypto runtime self-check failed")

// RuntimeSelfCheck verifies that Rust-backed crypto primitives are operational.
func RuntimeSelfCheck() error {
	if !runtimeSelfCheckCompare([]byte{0xA5}, []byte{0xA5}) {
		return ErrRuntimeSelfCheck
	}
	return nil
}
