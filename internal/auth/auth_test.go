package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"

	"github.com/alphabot-ai/slashbot/internal/model"
	"github.com/alphabot-ai/slashbot/internal/store/sqlite"
)

func TestEd25519Challenge(t *testing.T) {
	st, err := sqlite.Open("file:auth_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st, time.Hour, time.Minute)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	challenge, err := svc.CreateChallenge(context.Background(), "ed25519")
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}

	sig := ed25519.Sign(priv, []byte(challenge.Challenge))
	token, account, err := svc.VerifyAndCreateToken(
		context.Background(),
		"ed25519",
		base64.RawStdEncoding.EncodeToString(pub),
		challenge.Challenge,
		base64.RawStdEncoding.EncodeToString(sig),
	)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if account != nil {
		t.Fatalf("expected no account, got %+v", account)
	}
	if token.Token == "" {
		t.Fatalf("expected token")
	}
}

func TestTokenExpiration(t *testing.T) {
	st, err := sqlite.Open("file:auth_token_expire?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st, -1*time.Second, time.Minute)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	challenge, err := svc.CreateChallenge(context.Background(), "ed25519")
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}

	sig := ed25519.Sign(priv, []byte(challenge.Challenge))
	token, _, err := svc.VerifyAndCreateToken(
		context.Background(),
		"ed25519",
		base64.RawStdEncoding.EncodeToString(pub),
		challenge.Challenge,
		base64.RawStdEncoding.EncodeToString(sig),
	)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if _, err := svc.Authenticate(context.Background(), token.Token); err == nil {
		t.Fatalf("expected token expiration error")
	}
}

func TestRevokedKeyRejected(t *testing.T) {
	st, err := sqlite.Open("file:auth_revoked?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st, time.Hour, time.Minute)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubStr := base64.RawStdEncoding.EncodeToString(pub)

	account := model.Account{DisplayName: "Bot", CreatedAt: time.Now()}
	key := model.AccountKey{Alg: "ed25519", PublicKey: pubStr, CreatedAt: time.Now()}
	accountID, keyID, err := st.CreateAccount(context.Background(), &account, &key)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := st.RevokeAccountKey(context.Background(), accountID, keyID, time.Now()); err != nil {
		t.Fatalf("revoke key: %v", err)
	}

	challenge, err := svc.CreateChallenge(context.Background(), "ed25519")
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	sig := ed25519.Sign(priv, []byte(challenge.Challenge))
	_, _, err = svc.VerifyAndCreateToken(
		context.Background(),
		"ed25519",
		pubStr,
		challenge.Challenge,
		base64.RawStdEncoding.EncodeToString(sig),
	)
	if err == nil {
		t.Fatalf("expected revoked key error")
	}
}
