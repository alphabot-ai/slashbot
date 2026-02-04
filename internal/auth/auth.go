package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/alphabot-ai/slashbot/internal/model"
	"github.com/alphabot-ai/slashbot/internal/store"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/sha3"
)

type Service struct {
	store        store.Store
	tokenTTL     time.Duration
	challengeTTL time.Duration
}

type Verified struct {
	AccountID *int64
	KeyID     int64
}

func NewService(store store.Store, tokenTTL, challengeTTL time.Duration) *Service {
	return &Service{
		store:        store,
		tokenTTL:     tokenTTL,
		challengeTTL: challengeTTL,
	}
}

func (s *Service) CreateChallenge(ctx context.Context, alg string) (model.Challenge, error) {
	challenge, err := randomToken(32)
	if err != nil {
		return model.Challenge{}, err
	}
	c := model.Challenge{
		Challenge: challenge,
		Alg:       alg,
		ExpiresAt: time.Now().Add(s.challengeTTL),
	}
	if err := s.store.CreateChallenge(ctx, c); err != nil {
		return model.Challenge{}, err
	}
	return c, nil
}

func (s *Service) VerifyAndCreateToken(ctx context.Context, alg, publicKey, challenge, signature string) (model.Token, *model.Account, error) {
	c, err := s.store.ConsumeChallenge(ctx, challenge)
	if err != nil {
		return model.Token{}, nil, err
	}
	if time.Now().After(c.ExpiresAt) {
		return model.Token{}, nil, errors.New("challenge expired")
	}
	if c.Alg != alg {
		return model.Token{}, nil, errors.New("challenge alg mismatch")
	}

	if err := VerifySignature(alg, publicKey, challenge, signature); err != nil {
		return model.Token{}, nil, err
	}

	key, account, err := s.store.FindAccountKey(ctx, alg, publicKey)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return model.Token{}, nil, err
		}
		key = model.AccountKey{ID: 0}
		account = nil
	}
	if key.RevokedAt != nil {
		return model.Token{}, nil, errors.New("key revoked")
	}

	tokenValue, err := randomToken(32)
	if err != nil {
		return model.Token{}, nil, err
	}

	var accountID *int64
	var keyID int64
	if account != nil {
		accountID = &account.ID
		keyID = key.ID
	}

	token := model.Token{
		Token:     tokenValue,
		AccountID: accountID,
		KeyID:     keyID,
		ExpiresAt: time.Now().Add(s.tokenTTL),
	}
	if err := s.store.CreateToken(ctx, token); err != nil {
		return model.Token{}, nil, err
	}

	return token, account, nil
}

func (s *Service) Authenticate(ctx context.Context, bearer string) (Verified, error) {
	token, err := s.store.GetToken(ctx, bearer)
	if err != nil {
		return Verified{}, err
	}
	if time.Now().After(token.ExpiresAt) {
		return Verified{}, errors.New("token expired")
	}
	return Verified{AccountID: token.AccountID, KeyID: token.KeyID}, nil
}

func VerifySignature(alg, publicKey, message, signature string) error {
	switch strings.ToLower(alg) {
	case "ed25519":
		pubKey, sig, err := decodeEd25519(publicKey, signature)
		if err != nil {
			return err
		}
		if !ed25519.Verify(pubKey, []byte(message), sig) {
			return errors.New("invalid ed25519 signature")
		}
		return nil
	case "secp256k1":
		pubKeyBytes, sigBytes, err := decodeHexPair(publicKey, signature)
		if err != nil {
			return err
		}
		pubKey, err := secp256k1.ParsePubKey(pubKeyBytes)
		if err != nil {
			return err
		}
		if len(sigBytes) < 64 {
			return errors.New("invalid secp256k1 signature length")
		}
		r := new(big.Int).SetBytes(sigBytes[:32])
		s := new(big.Int).SetBytes(sigBytes[32:64])
		ethHash := ethereumPersonalHash([]byte(message))
		// Use ECDSA verify on the parsed pubkey.
		if !ecdsaVerify(pubKey, ethHash, r, s) {
			return errors.New("invalid secp256k1 signature")
		}
		return nil
	case "rsa-pss", "rsa-sha256":
		pubKey, sig, err := decodeRSA(publicKey, signature)
		if err != nil {
			return err
		}
		h := sha256.Sum256([]byte(message))
		if strings.ToLower(alg) == "rsa-pss" {
			if err := rsa.VerifyPSS(pubKey, crypto.SHA256, h[:], sig, nil); err != nil {
				return errors.New("invalid rsa-pss signature")
			}
			return nil
		}
		if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, h[:], sig); err != nil {
			return errors.New("invalid rsa signature")
		}
		return nil
	default:
		return fmt.Errorf("unsupported alg: %s", alg)
	}
}

func decodeEd25519(pub, sig string) (ed25519.PublicKey, []byte, error) {
	pubBytes, err := decodeBase64OrHex(pub)
	if err != nil {
		return nil, nil, err
	}
	sigBytes, err := decodeBase64OrHex(sig)
	if err != nil {
		return nil, nil, err
	}
	if l := len(pubBytes); l != ed25519.PublicKeySize {
		return nil, nil, errors.New("invalid ed25519 public key length")
	}
	if l := len(sigBytes); l != ed25519.SignatureSize {
		return nil, nil, errors.New("invalid ed25519 signature length")
	}
	return ed25519.PublicKey(pubBytes), sigBytes, nil
}

func decodeRSA(pub, sig string) (*rsa.PublicKey, []byte, error) {
	pubStr := strings.TrimSpace(pub)
	var pubKey *rsa.PublicKey
	if strings.HasPrefix(pubStr, "-----BEGIN") {
		block, _ := pem.Decode([]byte(pubStr))
		if block == nil {
			return nil, nil, errors.New("invalid pem public key")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err == nil {
			if pk, ok := parsed.(*rsa.PublicKey); ok {
				pubKey = pk
			}
		}
		if pubKey == nil {
			pk, err := x509.ParsePKCS1PublicKey(block.Bytes)
			if err != nil {
				return nil, nil, errors.New("unsupported rsa public key")
			}
			pubKey = pk
		}
	} else {
		pubBytes, err := decodeBase64OrHex(pubStr)
		if err != nil {
			return nil, nil, err
		}
		parsed, err := x509.ParsePKIXPublicKey(pubBytes)
		if err != nil {
			return nil, nil, err
		}
		pk, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, nil, errors.New("unsupported rsa public key")
		}
		pubKey = pk
	}

	sigBytes, err := decodeBase64OrHex(sig)
	if err != nil {
		return nil, nil, err
	}
	return pubKey, sigBytes, nil
}

func decodeHexPair(pub, sig string) ([]byte, []byte, error) {
	pubBytes, err := decodeHex(pub)
	if err != nil {
		return nil, nil, err
	}
	sigBytes, err := decodeHex(sig)
	if err != nil {
		return nil, nil, err
	}
	return pubBytes, sigBytes, nil
}

func decodeBase64OrHex(input string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(input); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(input); err == nil {
		return b, nil
	}
	return decodeHex(input)
}

func decodeHex(input string) ([]byte, error) {
	clean := strings.TrimPrefix(strings.TrimSpace(input), "0x")
	return hex.DecodeString(clean)
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ethereumPersonalHash(msg []byte) []byte {
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msg))
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(prefix))
	h.Write(msg)
	return h.Sum(nil)
}

func ecdsaVerify(pubKey *secp256k1.PublicKey, hash []byte, r, s *big.Int) bool {
	ecdsaPub := pubKey.ToECDSA()
	return ecdsa.Verify(ecdsaPub, hash, r, s)
}
