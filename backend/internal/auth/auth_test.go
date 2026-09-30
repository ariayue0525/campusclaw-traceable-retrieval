package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const secret = "test-secret-that-is-long-enough-1234567890"

func TestSignVerifyRoundTrip(t *testing.T) {
	now := time.Now()
	token, err := Sign(secret, Claims{Sub: 7, Username: "teacher-a", ClassID: 1, Role: "teacher", IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()})
	if err != nil { t.Fatal(err) }
	if strings.Count(token, ".") != 2 { t.Fatalf("expected three JWT segments, got %q", token) }
	claims, err := Verify(secret, token)
	if err != nil { t.Fatal(err) }
	if claims.Sub != 7 || claims.Username != "teacher-a" || claims.ClassID != 1 || claims.Role != "teacher" {
		t.Fatalf("claims round-trip mismatch: %+v", claims)
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	now := time.Now()
	token, _ := Sign(secret, Claims{Sub: 1, ExpiresAt: now.Add(time.Hour).Unix()})
	parts := strings.Split(token, ".")
	forged := parts[0] + "." + parts[1] + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := Verify(secret, forged); err == nil { t.Fatal("forged signature must fail") }
	// payload 被改动（签名未重算）同样拒绝。
	raw, err := base64RawURL(parts[1])
	if err != nil { t.Fatal(err) }
	raw[0] ^= 0xff
	// 无法直接改 payload 后保持签名，直接验证「换 payload 不换签名」。
	tampered := parts[0] + "." + b64(raw) + "." + parts[2]
	if _, err := Verify(secret, tampered); err == nil { t.Fatal("tampered payload must fail") }
}

func TestVerifyRejectsExpired(t *testing.T) {
	now := time.Now()
	token, _ := Sign(secret, Claims{Sub: 1, ExpiresAt: now.Add(-time.Minute).Unix()})
	if _, err := Verify(secret, token); err == nil { t.Fatal("expired token must fail") }
}

func TestVerifyRejectsAlgNone(t *testing.T) {
	// 手工构造 alg:none 的 token，签名部分随便填。
	header := b64([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := b64([]byte(`{"sub":1,"exp":99999999999}`))
	token := header + "." + payload + "." + b64([]byte("whatever"))
	if _, err := Verify(secret, token); err == nil { t.Fatal("alg none must fail") }
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, tok := range []string{"", "abc", "a.b", "a.b.c.d", "...."} {
		if _, err := Verify(secret, tok); err == nil {
			t.Fatalf("garbage token %q must fail", tok)
		}
	}
}

func base64RawURL(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
