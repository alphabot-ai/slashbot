package httpapp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/alphabot-ai/slashbot/internal/auth"
	"github.com/alphabot-ai/slashbot/internal/client"
	"github.com/alphabot-ai/slashbot/internal/config"
	httpapp "github.com/alphabot-ai/slashbot/internal/http"
	"github.com/alphabot-ai/slashbot/internal/rate"
	"github.com/alphabot-ai/slashbot/internal/store/sqlite"
)

func TestEndToEndServer(t *testing.T) {
	st, err := sqlite.Open("file:e2e_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cfg := config.Config{
		Addr:         ":0",
		RateLimits:   config.RateLimits{StoryPerMinute: 1000, CommentPerMinute: 1000, VotePerMinute: 1000},
		HashSecret:   "test-hash",
		AdminSecret:  "admin",
		TokenTTL:     time.Hour,
		ChallengeTTL: time.Minute,
	}
	limiter := rate.NewMemory()
	authSvc := auth.NewService(st, cfg.TokenTTL, cfg.ChallengeTTL)
	server, err := httpapp.NewServer(st, authSvc, limiter, cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	httpServer := &http.Server{Handler: server}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	defer httpServer.Close()

	baseURL := "http://" + listener.Addr().String()

	// Create account and get token using client package
	helper := client.NewTestHelper(baseURL)
	token, err := helper.GetToken("e2e-account")
	if err != nil {
		t.Fatalf("create e2e token: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"title": "E2E Story",
		"url":   "https://example.com",
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/stories", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post story: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("post story status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, baseURL+"/", nil)
	req.Header.Set("Accept", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get home: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("home status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()
}
