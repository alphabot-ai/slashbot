package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/alphabot-ai/slashbot/internal/model"
	"github.com/alphabot-ai/slashbot/internal/store"
)

func TestAccountKeys(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	account := model.Account{DisplayName: "Bot", CreatedAt: time.Now()}
	key := model.AccountKey{Alg: "ed25519", PublicKey: "pubkey", CreatedAt: time.Now()}

	accountID, keyID, err := st.CreateAccount(context.Background(), &account, &key)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if accountID == 0 || keyID == 0 {
		t.Fatalf("expected ids")
	}

	k, acc, err := st.FindAccountKey(context.Background(), "ed25519", "pubkey")
	if err != nil {
		t.Fatalf("find key: %v", err)
	}
	if acc == nil || acc.ID != accountID {
		t.Fatalf("expected account")
	}
	if k.ID != keyID {
		t.Fatalf("expected key id")
	}

	_, err = st.AddAccountKey(context.Background(), accountID, &model.AccountKey{Alg: "ed25519", PublicKey: "pubkey", CreatedAt: time.Now()})
	if err == nil {
		t.Fatalf("expected duplicate key error")
	}
	if err != store.ErrDuplicateKey {
		t.Fatalf("expected ErrDuplicateKey, got %v", err)
	}

	if err := st.RevokeAccountKey(context.Background(), accountID, keyID, time.Now()); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	k, _, err = st.FindAccountKey(context.Background(), "ed25519", "pubkey")
	if err != nil {
		t.Fatalf("find key after revoke: %v", err)
	}
	if k.RevokedAt == nil {
		t.Fatalf("expected revoked_at set")
	}
}
