package crypto

// PKCEChallenge computes the PKCE challenge for the provided verifier and method.
func PKCEChallenge(verifier, method string) (string, error) {
	return cPKCEChallenge(verifier, method)
}

// VerifyPKCE verifies a PKCE verifier against the provided challenge.
func VerifyPKCE(verifier, challenge, method string) (bool, error) {
	return cPKCEVerify(verifier, challenge, method)
}
