// Package auth 实现课程示例所用的 JWT Bearer 凭证：
// HS256 签名、payload 携带用户标识与过期时间，服务端无状态校验。
// 不引入第三方依赖——签名验证就是一次 HMAC-SHA256 加三段 base64url。
package auth

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

// ErrInvalid 覆盖一切不可信情形：格式错误、算法不符、签名不匹配、已过期。
// 调用方统一映射为 401，不区分细节，避免向客户端泄露失败原因。
var ErrInvalid = errors.New("invalid token")

// Leeway 是验证 exp 时允许的时钟偏移。
const Leeway = 30 * time.Second

// Claims 是 access token 的负载。ClassID 与 Role 只用于展示与调试比对，
// 授权一律以服务端按 Sub 回库读取的实时身份为准。
type Claims struct {
	Sub      int64  `json:"sub"`
	Username string `json:"username"`
	ClassID  int64  `json:"class_id"`
	Role     string `json:"role"`
	IssuedAt int64  `json:"iat"`
	ExpiresAt int64 `json:"exp"`
}

func (c Claims) valid(now time.Time) bool {
	if c.Sub < 1 { return false }
	if c.ExpiresAt <= 0 { return false }
	return now.Add(Leeway).Unix() < c.ExpiresAt
}

func b64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

// Sign 用 HS256 签发 token。secret 为 SESSION_SECRET。
func Sign(secret string, c Claims) (string, error) {
	if strings.TrimSpace(secret) == "" { return "", errors.New("auth: empty secret") }
	if c.IssuedAt == 0 { c.IssuedAt = time.Now().Unix() }
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil { return "", err }
	payload, err := json.Marshal(c)
	if err != nil { return "", err }
	signing := b64(header) + "." + b64(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signing))
	return signing + "." + b64(mac.Sum(nil)), nil
}

// Verify 校验 token：格式、算法（必须 HS256）、签名与 exp。
func Verify(secret, token string) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 { return claims, ErrInvalid }
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil { return claims, ErrInvalid }
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Alg != "HS256" {
		// 明确拒绝 alg:none 一类降级尝试。
		return claims, fmt.Errorf("%w: unsupported alg", ErrInvalid)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, got) { return claims, ErrInvalid }
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return claims, ErrInvalid }
	if err := json.Unmarshal(payloadRaw, &claims); err != nil { return claims, ErrInvalid }
	if !claims.valid(time.Now()) { return claims, ErrInvalid }
	return claims, nil
}
