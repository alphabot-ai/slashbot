package httpapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"slashbot/internal/auth"
	"slashbot/internal/client"
	"slashbot/internal/config"
	"slashbot/internal/model"
	"slashbot/internal/store/sqlite"
)

type allowAllLimiter struct{}

func (a allowAllLimiter) Allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	return true, 0
}

func TestHomeJSON(t *testing.T) {
	st, err := sqlite.Open("file:http_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	story := model.Story{
		Title:     "Test Story",
		URL:       "https://example.com",
		CreatedAt: time.Now(),
	}
	if _, err := st.CreateStory(context.Background(), &story); err != nil {
		t.Fatalf("create story: %v", err)
	}

	cfg := config.Config{RateLimits: config.RateLimits{StoryPerMinute: 100, CommentPerMinute: 100, VotePerMinute: 100}}
	authSvc := auth.NewService(st, time.Hour, time.Minute)
	server, err := NewServer(st, authSvc, allowAllLimiter{}, cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	resp := httptest.NewRecorder()

	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if _, ok := payload["stories"]; !ok {
		t.Fatalf("expected stories field")
	}
}

func TestCreateStoryJSON(t *testing.T) {
	st, err := sqlite.Open("file:http_test_create?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cfg := config.Config{RateLimits: config.RateLimits{StoryPerMinute: 100, CommentPerMinute: 100, VotePerMinute: 100}}
	authSvc := auth.NewService(st, time.Hour, time.Minute)
	server, err := NewServer(st, authSvc, allowAllLimiter{}, cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	// Start an actual test server so the client can connect
	ts := httptest.NewServer(server)
	defer ts.Close()

	// Create account and get token using client package
	helper := client.NewTestHelper(ts.URL)
	token, err := helper.GetToken("test-account")
	if err != nil {
		t.Fatalf("create test token: %v", err)
	}

	body := `{"title":"A Valid Story","url":"https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/stories", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}
