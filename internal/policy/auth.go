package policy

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

// Authenticate проверяет credential без внешнего identity provider. JWT
// поддерживает только HS256; для OIDC/JWKS следует подключить отдельный verifier.
func Authenticate(r *http.Request, auth *config.Auth) (string, error) {
	if auth == nil {
		return "", errors.New("auth config отсутствует")
	}
	switch auth.Type {
	case "api_key":
		actual := r.Header.Get(auth.Header)
		if subtle.ConstantTimeCompare([]byte(actual), []byte(auth.Value)) != 1 {
			return "", errors.New("неверный API key")
		}
		return actual, nil
	case "basic":
		username, password, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(username), []byte(auth.Username)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(auth.Password)) != 1 {
			return "", errors.New("неверные Basic credentials")
		}
		return username, nil
	case "jwt_hs256":
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get(auth.Header), "Bearer "))
		return verifyJWT(token, auth, time.Now())
	default:
		return "", errors.New("неподдерживаемый тип auth")
	}
}

type jwtClaims struct {
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	Audience  any    `json:"aud"`
	ExpiresAt int64  `json:"exp"`
	NotBefore int64  `json:"nbf"`
}

func verifyJWT(token string, auth *config.Auth, now time.Time) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("некорректный JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("некорректный JWT header")
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if json.Unmarshal(headerBytes, &header) != nil || header.Algorithm != "HS256" {
		return "", errors.New("разрешён только JWT HS256")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", errors.New("некорректная JWT signature")
	}
	mac := hmac.New(sha256.New, []byte(auth.JWTSecret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", errors.New("неверная JWT signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("некорректный JWT payload")
	}
	var claims jwtClaims
	if err = json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("JWT claims: %w", err)
	}
	unix := now.Unix()
	if claims.ExpiresAt > 0 && unix >= claims.ExpiresAt {
		return "", errors.New("JWT истёк")
	}
	if claims.NotBefore > 0 && unix < claims.NotBefore {
		return "", errors.New("JWT ещё не активен")
	}
	if auth.JWTIssuer != "" && claims.Issuer != auth.JWTIssuer {
		return "", errors.New("неверный JWT issuer")
	}
	if auth.JWTAudience != "" && !audienceContains(claims.Audience, auth.JWTAudience) {
		return "", errors.New("неверный JWT audience")
	}
	return claims.Subject, nil
}

func audienceContains(value any, expected string) bool {
	switch audience := value.(type) {
	case string:
		return audience == expected
	case []any:
		for _, item := range audience {
			if text, ok := item.(string); ok && text == expected {
				return true
			}
		}
	}
	return false
}
