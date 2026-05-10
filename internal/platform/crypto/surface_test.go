package crypto

import (
	"net/http"
	"testing"
	"time"
)

func TestOpaqueHelpers(t *testing.T) {
	hash, err := HashOpaqueToken("opaque-token-value")
	if err != nil {
		t.Fatalf("HashOpaqueToken() error = %v", err)
	}
	if hash == "" {
		t.Fatalf("HashOpaqueToken() returned empty hash")
	}
	if !ValidateOpaqueToken("opaque-token-value", hash) {
		t.Fatalf("ValidateOpaqueToken() expected true")
	}
	if ValidateOpaqueToken("other-token", hash) {
		t.Fatalf("ValidateOpaqueToken() expected false for mismatched token")
	}
	if ValidateOpaqueToken("", hash) || ValidateOpaqueToken("token", "") {
		t.Fatalf("ValidateOpaqueToken() should reject empty inputs")
	}

	prefix, err := OpaqueTokenPrefix("opaque-token-value", 12)
	if err != nil {
		t.Fatalf("OpaqueTokenPrefix() error = %v", err)
	}
	if prefix == "" {
		t.Fatalf("OpaqueTokenPrefix() returned empty prefix")
	}
	emptyPrefix, err := OpaqueTokenPrefix("opaque-token-value", 0)
	if err != nil || emptyPrefix != "" {
		t.Fatalf("OpaqueTokenPrefix(length=0) = %q, %v; want empty,nil", emptyPrefix, err)
	}
}

func TestEnvelopeAndPKCEHelpers(t *testing.T) {
	secret := []byte("envelope-secret")
	payload := []byte("payload")
	now := time.Now().UTC()

	digest, err := HMACSHA256Hex(secret, payload)
	if err != nil {
		t.Fatalf("HMACSHA256Hex() error = %v", err)
	}
	if len(digest) != 64 {
		t.Fatalf("HMACSHA256Hex() len = %d, want 64", len(digest))
	}

	envelope, err := SignEnvelope("test_kind", secret, payload, now)
	if err != nil {
		t.Fatalf("SignEnvelope() error = %v", err)
	}
	if !VerifyEnvelope("test_kind", secret, payload, envelope, time.Minute, now.Add(10*time.Second)) {
		t.Fatalf("VerifyEnvelope() expected true")
	}
	if VerifyEnvelope("test_kind", secret, []byte("tampered"), envelope, time.Minute, now.Add(10*time.Second)) {
		t.Fatalf("VerifyEnvelope() expected false for tampered payload")
	}
	if VerifyEnvelope("test_kind", []byte("wrong-secret"), payload, envelope, time.Minute, now.Add(10*time.Second)) {
		t.Fatalf("VerifyEnvelope() expected false for wrong secret")
	}
	verifier := "4M6Jv90v9QvW8rPqstNqA4HVh8jFhQMM5k52nNc8QYg"
	challenge, err := PKCEChallenge(verifier, "S256")
	if err != nil {
		t.Fatalf("PKCEChallenge() error = %v", err)
	}
	if challenge == "" {
		t.Fatalf("PKCEChallenge() returned empty challenge")
	}
	ok, err := VerifyPKCE(verifier, challenge, "S256")
	if err != nil {
		t.Fatalf("VerifyPKCE(valid) error = %v", err)
	}
	if !ok {
		t.Fatalf("VerifyPKCE(valid) expected true")
	}
	ok, err = VerifyPKCE("wrong", challenge, "S256")
	if err != nil {
		t.Fatalf("VerifyPKCE(invalid) error = %v", err)
	}
	if ok {
		t.Fatalf("VerifyPKCE(invalid) expected false")
	}
}

func TestGoNativeCompatibilityGoldenVectors(t *testing.T) {
	secret := []byte("secret")
	payload := []byte("payload")
	fixedUnix := time.Unix(1700000000, 0).UTC()
	fixedMillis := time.UnixMilli(1700000000000).UTC()

	hmacHex, err := HMACSHA256Hex(secret, payload)
	if err != nil {
		t.Fatalf("HMACSHA256Hex() error = %v", err)
	}
	if hmacHex != "b82fcb791acec57859b989b430a826488ce2e479fdf92326bd0a2e8375a42ba4" {
		t.Fatalf("HMACSHA256Hex() = %q", hmacHex)
	}

	envelope, err := SignEnvelope("identity", secret, payload, fixedUnix)
	if err != nil {
		t.Fatalf("SignEnvelope() error = %v", err)
	}
	if envelope != "v1;t=1700000000;s=ddee276d8ae867e7da775ca48e4097426709cd7cc875c8f8c57f41bfa16f6210" {
		t.Fatalf("SignEnvelope() = %q", envelope)
	}
	if !VerifyEnvelope("identity", secret, payload, envelope, time.Minute, fixedUnix.Add(30*time.Second)) {
		t.Fatalf("VerifyEnvelope() expected true for golden vector")
	}

	signedCookie, err := SignSessionCookieValue(secret, "token", fixedUnix)
	if err != nil {
		t.Fatalf("SignSessionCookieValue() error = %v", err)
	}
	if signedCookie != "v1.dG9rZW4.1700000000.476bb9ee9d68bc2405a3e8b979b770d8f638222b7b1ad82dfefb2d07858c92f3" {
		t.Fatalf("SignSessionCookieValue() = %q", signedCookie)
	}

	internalToken, err := GenerateInternalToken(secret, "admin", fixedMillis)
	if err != nil {
		t.Fatalf("GenerateInternalToken() error = %v", err)
	}
	if internalToken != "v1.YWRtaW4.1700000000000.223cef548aea2647bff609d3b21677da3bb2926494ad7b5b2f8cf580b257ac8a" {
		t.Fatalf("GenerateInternalToken() = %q", internalToken)
	}

	opaqueHash, err := HashOpaqueToken("opaque-token")
	if err != nil {
		t.Fatalf("HashOpaqueToken() error = %v", err)
	}
	if opaqueHash != "hNPyPam19RsyaVZu/wXT+yNgfu74lWf5zSgLkMoNvFw=" {
		t.Fatalf("HashOpaqueToken() = %q", opaqueHash)
	}

	pkceChallenge, err := PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", "S256")
	if err != nil {
		t.Fatalf("PKCEChallenge() error = %v", err)
	}
	if pkceChallenge != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("PKCEChallenge() = %q", pkceChallenge)
	}
}

func TestSessionAndIdentitySigningHelpers(t *testing.T) {
	secret := []byte("session-secret")
	now := time.Now().UTC()

	signedCookie, err := SignSessionCookieValue(secret, "session-token", now)
	if err != nil {
		t.Fatalf("SignSessionCookieValue() error = %v", err)
	}
	token, err := VerifySessionCookieValue(secret, signedCookie, time.Minute, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("VerifySessionCookieValue() error = %v", err)
	}
	if token != "session-token" {
		t.Fatalf("VerifySessionCookieValue() token = %q, want session-token", token)
	}
	if _, err := VerifySessionCookieValue(secret, signedCookie+"tampered", time.Minute, now.Add(5*time.Second)); err == nil {
		t.Fatalf("VerifySessionCookieValue(tampered) expected error")
	}

	headers := map[string]string{
		"X-Session-ID":   " sid-1 ",
		"x-auth-time":    " 123 ",
		"X-Empty-Header": "",
	}
	payload := string(CanonicalSessionContextPayload(headers))
	if payload != "x-empty-header:\nx-session-id:sid-1\nx-auth-time:123" {
		t.Fatalf("CanonicalSessionContextPayload() = %q", payload)
	}
	envelope, err := SignSessionContextHeaders(secret, headers, now)
	if err != nil {
		t.Fatalf("SignSessionContextHeaders() error = %v", err)
	}
	if !VerifySessionContextHeaders(secret, headers, envelope, now.Add(10*time.Second)) {
		t.Fatalf("VerifySessionContextHeaders() expected true")
	}
	headers["X-Session-ID"] = "sid-2"
	if VerifySessionContextHeaders(secret, headers, envelope, now.Add(10*time.Second)) {
		t.Fatalf("VerifySessionContextHeaders() expected false for mutated headers")
	}

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("X-One", " 1 ")
	req.Header.Set("X-Two", "2")
	headerMap := ReadRequestHeaderMap(req, []string{"X-One", "X-Two", "Missing"})
	if len(headerMap) != 3 || headerMap["X-One"] != " 1 " || headerMap["Missing"] != "" {
		t.Fatalf("ReadRequestHeaderMap() unexpected result: %#v", headerMap)
	}

	identityHeaders := http.Header{}
	identityHeaders.Set("X-Subject", " user-1 ")
	identityHeaders.Set("X-Actor", " actor-1 ")
	identityPayload := string(CanonicalHeaderPayload(identityHeaders, []string{"X-Actor", "X-Subject", "Missing"}))
	if identityPayload != "x-actor:actor-1\nx-subject:user-1" {
		t.Fatalf("CanonicalHeaderPayload() = %q", identityPayload)
	}
	emptyEnv, err := SignIdentityHeaders(secret, http.Header{}, []string{"X-Subject"}, now)
	if err != nil || emptyEnv != "" {
		t.Fatalf("SignIdentityHeaders(empty) = %q, %v; want empty,nil", emptyEnv, err)
	}
	identityEnv, err := SignIdentityHeaders(secret, identityHeaders, []string{"X-Subject", "X-Actor"}, now)
	if err != nil {
		t.Fatalf("SignIdentityHeaders() error = %v", err)
	}
	if !VerifyIdentityHeaders(secret, identityHeaders, []string{"X-Subject", "X-Actor"}, identityEnv, time.Minute, now.Add(10*time.Second)) {
		t.Fatalf("VerifyIdentityHeaders() expected true")
	}
	identityHeaders.Set("X-Subject", "user-2")
	if VerifyIdentityHeaders(secret, identityHeaders, []string{"X-Subject", "X-Actor"}, identityEnv, time.Minute, now.Add(10*time.Second)) {
		t.Fatalf("VerifyIdentityHeaders() expected false for mutated headers")
	}
}

func TestInternalTokenHelpers(t *testing.T) {
	secret := []byte("internal-secret")
	now := time.Now().UTC()
	token, err := GenerateInternalToken(secret, "admin", now)
	if err != nil {
		t.Fatalf("GenerateInternalToken() error = %v", err)
	}
	if token == "" {
		t.Fatalf("GenerateInternalToken() returned empty token")
	}

	valid, err := VerifyInternalToken(secret, token, time.Second, now.Add(2*time.Millisecond))
	if err != nil {
		t.Fatalf("VerifyInternalToken(valid) error = %v", err)
	}
	if valid.ModuleID != "admin" || len(valid.Signature) == 0 || !valid.Timestamp.Equal(valid.Timestamp.UTC()) {
		t.Fatalf("VerifyInternalToken(valid) unexpected result: %#v", valid)
	}

	if _, err := VerifyInternalToken(secret, token, time.Nanosecond, now); err != nil {
		t.Fatalf("VerifyInternalToken(ttl floor) error = %v", err)
	}

	if _, err := VerifyInternalToken(secret, token, time.Millisecond, now.Add(5*time.Second)); err == nil {
		t.Fatalf("VerifyInternalToken(expired) expected error")
	}
	if _, err := VerifyInternalToken([]byte("wrong-secret"), token, time.Minute, now.Add(2*time.Millisecond)); err == nil {
		t.Fatalf("VerifyInternalToken(wrong secret) expected error")
	}
	if _, err := VerifyInternalToken(secret, token+"tampered", time.Minute, now.Add(2*time.Millisecond)); err == nil {
		t.Fatalf("VerifyInternalToken(tampered token) expected error")
	}

	if valid.Timestamp.Location() != time.UTC {
		t.Fatalf("VerifyInternalToken(valid) timestamp location = %v, want UTC", valid.Timestamp.Location())
	}
}
