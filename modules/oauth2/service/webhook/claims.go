// Package webhook provides OAuth2 token claims injection via webhooks.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ClaimsRequest represents a request to inject claims into a token.
type ClaimsRequest struct {
	IdentityID string                 `json:"identity_id"`
	ClientID   string                 `json:"client_id"`
	Scopes     []string               `json:"scopes"`
	TokenType  string                 `json:"token_type"`       // "access", "id"
	Claims     map[string]interface{} `json:"claims,omitempty"` // Existing claims
}

// ClaimsResponse represents the response from the claims hook.
type ClaimsResponse struct {
	Claims map[string]interface{} `json:"claims"`
	Error  *string                `json:"error,omitempty"`
}

// ClaimsHookClient calls external webhook to inject custom claims.
type ClaimsHookClient struct {
	webhookURL string
	secret     string
	timeout    time.Duration
	client     *http.Client
}

// NewClaimsHookClient creates a new claims hook client.
func NewClaimsHookClient(webhookURL, secret string, timeout time.Duration) *ClaimsHookClient {
	return &ClaimsHookClient{
		webhookURL: webhookURL,
		secret:     secret,
		timeout:    timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// InjectClaims calls the webhook to inject custom claims.
func (c *ClaimsHookClient) InjectClaims(ctx context.Context, req *ClaimsRequest) (map[string]interface{}, error) {
	if c.webhookURL == "" {
		// No webhook configured, return existing claims
		if req.Claims != nil {
			return req.Claims, nil
		}
		return make(map[string]interface{}), nil
	}

	// Marshal request
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Aegion-OAuth2/1.0")

	// Sign request if secret is configured
	if c.secret != "" {
		signature := c.signPayload(payload)
		httpReq.Header.Set("X-Aegion-Signature", signature)
	}

	// Execute request
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var claimsResp ClaimsResponse
	if err := json.NewDecoder(resp.Body).Decode(&claimsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for error in response
	if claimsResp.Error != nil && *claimsResp.Error != "" {
		return nil, fmt.Errorf("webhook error: %s", *claimsResp.Error)
	}

	return claimsResp.Claims, nil
}

// signPayload generates HMAC-SHA256 signature for the payload.
func (c *ClaimsHookClient) signPayload(payload []byte) string {
	h := hmac.New(sha256.New, []byte(c.secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// VerifySignature verifies the HMAC signature of a webhook request.
func (c *ClaimsHookClient) VerifySignature(payload []byte, signature string) bool {
	expected := c.signPayload(payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// MergeClaimsHTTP is a helper to merge custom claims into existing claims (for HTTP integration).
func MergeClaimsHTTP(identityID, clientID, tokenType string, scopes []string, baseClaims map[string]interface{}, hookClient *ClaimsHookClient) (map[string]interface{}, error) {
	if hookClient == nil {
		return baseClaims, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &ClaimsRequest{
		IdentityID: identityID,
		ClientID:   clientID,
		Scopes:     scopes,
		TokenType:  tokenType,
		Claims:     baseClaims,
	}

	customClaims, err := hookClient.InjectClaims(ctx, req)
	if err != nil {
		// Log error but don't fail token issuance
		return baseClaims, fmt.Errorf("claims hook failed (non-fatal): %w", err)
	}

	// Merge custom claims into base claims
	for k, v := range customClaims {
		// Don't allow overriding reserved OIDC claims
		if !isReservedClaim(k) {
			baseClaims[k] = v
		}
	}

	return baseClaims, nil
}

// isReservedClaim checks if a claim is a reserved OIDC/OAuth2 claim.
func isReservedClaim(claim string) bool {
	reserved := map[string]bool{
		"iss": true, "sub": true, "aud": true, "exp": true,
		"iat": true, "auth_time": true, "nonce": true,
		"acr": true, "amr": true, "azp": true,
		"at_hash": true, "c_hash": true, "jti": true,
	}
	return reserved[claim]
}
