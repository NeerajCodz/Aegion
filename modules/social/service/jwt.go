package service

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	platformjwt "github.com/aegion/aegion/internal/platform/jwt"
)

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

func (s *Service) verifyAndDecodeIDToken(ctx context.Context, token, clientID, issuer, jwksURI string) (map[string]interface{}, error) {
	header, err := parseJWTHeader(token)
	if err != nil {
		return nil, err
	}
	keys, err := s.fetchJWKS(ctx, jwksURI)
	if err != nil {
		return nil, err
	}

	var matched *jwk
	for i := range keys {
		key := keys[i]
		if header.Kid != "" && key.Kid != "" && key.Kid != header.Kid {
			continue
		}
		if header.Alg != "" && key.Alg != "" && key.Alg != header.Alg {
			continue
		}
		matched = &key
		break
	}
	if matched == nil {
		return nil, errors.New("provider jwk not found")
	}

	publicKey, err := jwkToVerifyKey(*matched)
	if err != nil {
		return nil, err
	}
	if _, err := platformjwt.Verify(token, publicKey, header.Alg, platformjwt.VerifyOptions{
		Issuer:   issuer,
		Audience: clientID,
		Leeway:   shortLeeway(),
	}); err != nil {
		return nil, err
	}
	return parseJWTPayload(token)
}

func (s *Service) fetchJWKS(ctx context.Context, jwksURI string) (keys []jwk, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jwks fetch failed with status %d", resp.StatusCode)
	}

	var doc jwksDocument
	if err = json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	return doc.Keys, nil
}

func parseJWTHeader(token string) (jwtHeader, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtHeader{}, errors.New("invalid jwt")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return jwtHeader{}, err
	}
	var header jwtHeader
	if err := json.Unmarshal(payload, &header); err != nil {
		return jwtHeader{}, err
	}
	return header, nil
}

func parseJWTPayload(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid jwt")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func jwkToVerifyKey(key jwk) ([]byte, error) {
	switch key.Kty {
	case "EC":
		if strings.EqualFold(key.Crv, "P-256") {
			x, err := decodeBase64URLInt(key.X)
			if err != nil {
				return nil, err
			}
			y, err := decodeBase64URLInt(key.Y)
			if err != nil {
				return nil, err
			}
			xBytes := x.FillBytes(make([]byte, 32))
			yBytes := y.FillBytes(make([]byte, 32))
			return append([]byte{0x04}, append(xBytes, yBytes...)...), nil
		}
		return nil, errors.New("unsupported ec curve")
	case "RSA":
		modulus, err := decodeBase64URLInt(key.N)
		if err != nil {
			return nil, err
		}
		exponent, err := decodeBase64URLInt(key.E)
		if err != nil {
			return nil, err
		}
		pub := rsa.PublicKey{
			N: modulus,
			E: int(exponent.Int64()),
		}
		return x509.MarshalPKCS1PublicKey(&pub), nil
	default:
		return nil, errors.New("unsupported jwk type")
	}
}

func decodeBase64URLInt(input string) (*big.Int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(input))
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(decoded), nil
}
