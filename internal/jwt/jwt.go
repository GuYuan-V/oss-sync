// Package jwt 实现服务端使用的最小 HS256 令牌格式，不依赖第三方 JWT 库完成签发与解析。
package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid jwt token")
	ErrExpired      = errors.New("jwt token expired")
)

// DeviceID 为强类型的客户端设备标识。
type DeviceID string

// Claims 为 API 与 Web 认证共用的 JWT 载荷。
type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	// TokenVersion 使改密前签发的令牌失效。
	TokenVersion uint     `json:"tv"`
	DeviceID     DeviceID `json:"did,omitempty"`
	IssuedAt     int64    `json:"iat"`
	ExpAt        int64    `json:"exp"`
}

type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// Sign 按指定有效期签发 HS256 令牌。
func Sign(secret string, claims Claims, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("jwt secret is empty")
	}
	now := time.Now()
	if claims.IssuedAt == 0 {
		claims.IssuedAt = now.Unix()
	}
	claims.ExpAt = now.Add(ttl).Unix()

	h := header{Alg: "HS256", Typ: "JWT"}
	hBytes, _ := json.Marshal(h)
	payloadBytes, _ := json.Marshal(claims)

	seg1 := base64.RawURLEncoding.EncodeToString(hBytes)
	seg2 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := seg1 + "." + seg2
	sig := hmacSha256(secret, signingInput)
	return signingInput + "." + sig, nil
}

// Parse 校验 HS256 令牌签名并检查是否过期。
func Parse(secret, token string) (*Claims, error) {
	if secret == "" {
		return nil, errors.New("jwt secret is empty")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	// 仅接受 HS256，其余算法一律拒绝。
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var h header
	if err := json.Unmarshal(headerBytes, &h); err != nil {
		return nil, ErrInvalidToken
	}
	if h.Alg != "HS256" {
		return nil, ErrInvalidToken
	}
	signingInput := parts[0] + "." + parts[1]
	expectedSig := hmacSha256(secret, signingInput)
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, ErrInvalidToken
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(payloadBytes, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if c.ExpAt > 0 && time.Now().Unix() > c.ExpAt {
		return nil, ErrExpired
	}
	return &c, nil
}

func hmacSha256(secret, input string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// MustSign 为签发失败时直接 panic 的测试辅助函数。
func MustSign(secret string, claims Claims, ttl time.Duration) string {
	t, err := Sign(secret, claims, ttl)
	if err != nil {
		panic(fmt.Sprintf("jwt.Sign failed: %v", err))
	}
	return t
}
