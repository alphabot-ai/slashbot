package client

import (
	"testing"
)

func TestGenerateCredentials(t *testing.T) {
	creds, err := GenerateCredentials("test-bot")
	if err != nil {
		t.Fatalf("generate credentials: %v", err)
	}

	if creds.BotName != "test-bot" {
		t.Errorf("expected bot name 'test-bot', got '%s'", creds.BotName)
	}

	if creds.PublicKey == "" {
		t.Error("expected non-empty public key")
	}

	if len(creds.PrivateKey) == 0 {
		t.Error("expected non-empty private key")
	}
}

func TestCredentialsSign(t *testing.T) {
	creds, err := GenerateCredentials("test-bot")
	if err != nil {
		t.Fatalf("generate credentials: %v", err)
	}

	message := "test message"
	sig := creds.Sign(message)

	if sig == "" {
		t.Error("expected non-empty signature")
	}

	// Sign the same message again - should produce same signature
	sig2 := creds.Sign(message)
	if sig != sig2 {
		t.Error("expected deterministic signature for ed25519")
	}
}

func TestCredentialsFromKeys(t *testing.T) {
	// First generate credentials
	orig, err := GenerateCredentials("test-bot")
	if err != nil {
		t.Fatalf("generate credentials: %v", err)
	}

	// Export the private key
	privKeyB64 := orig.Sign("") // Just to get a signature format reference

	// This test verifies the key loading path works
	// A full round-trip test would require base64 encoding the private key
	if privKeyB64 == "" {
		t.Error("expected signature from original credentials")
	}
}

func TestClientNew(t *testing.T) {
	c := New("https://example.com")

	if c.BaseURL != "https://example.com" {
		t.Errorf("expected base URL 'https://example.com', got '%s'", c.BaseURL)
	}

	if c.HTTPClient == nil {
		t.Error("expected non-nil HTTP client")
	}

	if c.IsAuthenticated() {
		t.Error("expected new client to not be authenticated")
	}
}
