package httpapp

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alphabot-ai/slashbot/internal/auth"
	"github.com/alphabot-ai/slashbot/internal/client"
	"github.com/alphabot-ai/slashbot/internal/config"
	"github.com/alphabot-ai/slashbot/internal/model"
	"github.com/alphabot-ai/slashbot/internal/rate"
	"github.com/alphabot-ai/slashbot/internal/store/sqlite"
)

type testClient struct {
	server *httptest.Server
	client *http.Client
}

func newTestClient(t *testing.T) *testClient {
	t.Helper()
	cfg := config.Config{
		RateLimits:   config.RateLimits{StoryPerMinute: 1000, CommentPerMinute: 1000, VotePerMinute: 1000},
		HashSecret:   "test-hash",
		AdminSecret:  "admin",
		TokenTTL:     time.Hour,
		ChallengeTTL: time.Minute,
	}
	return newTestClientWithConfig(t, cfg)
}

func newTestClientWithConfig(t *testing.T, cfg config.Config) *testClient {
	t.Helper()
	if cfg.HashSecret == "" {
		cfg.HashSecret = "test-hash"
	}
	if cfg.AdminSecret == "" {
		cfg.AdminSecret = "admin"
	}
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = time.Hour
	}
	if cfg.ChallengeTTL == 0 {
		cfg.ChallengeTTL = time.Minute
	}
	dsnName := strings.NewReplacer("/", "_").Replace(t.Name())
	st, err := sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", dsnName))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	limiter := rate.NewMemory()
	authSvc := auth.NewService(st, cfg.TokenTTL, cfg.ChallengeTTL)
	server, err := NewServer(st, authSvc, limiter, cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(server)
	t.Cleanup(func() {
		ts.Close()
		_ = st.Close()
	})
	return &testClient{server: ts, client: ts.Client()}
}

func (c *testClient) postJSON(t *testing.T, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.server.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	return resp
}

func (c *testClient) get(t *testing.T, path string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response, out *T) {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("json decode: %v (body %s)", err, string(body))
	}
}

// createTestAccount creates an account and returns a valid access token
func createTestAccount(t *testing.T, tc *testClient, name string) string {
	t.Helper()
	helper := client.NewTestHelper(tc.server.URL)
	token, err := helper.GetToken(name)
	if err != nil {
		t.Fatalf("create test account: %v", err)
	}
	return token
}

func TestStoryCommentVoteFlow(t *testing.T) {
	client := newTestClient(t)

	// Create account and get token first
	token := createTestAccount(t, client, "flow-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Integration Story",
		"url":   "https://example.com",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story status %d: %s", resp.StatusCode, string(b))
	}
	var story model.Story
	decodeJSON(t, resp, &story)
	if story.ID == 0 {
		t.Fatalf("expected story id")
	}

	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "First!",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create comment status %d: %s", resp.StatusCode, string(b))
	}

	resp = client.postJSON(t, "/api/votes", map[string]any{
		"target_type": "story",
		"target_id":   story.ID,
		"value":       1,
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("vote status %d: %s", resp.StatusCode, string(b))
	}

	resp = client.get(t, "/stories/"+strconv.FormatInt(story.ID, 10), map[string]string{"Accept": "application/json"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get story status %d: %s", resp.StatusCode, string(b))
	}
	var payload map[string]any
	decodeJSON(t, resp, &payload)
	if _, ok := payload["story"]; !ok {
		t.Fatalf("expected story payload")
	}
}

func TestContentNegotiation(t *testing.T) {
	client := newTestClient(t)
	resp := client.get(t, "/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected html content-type, got %s", ct)
	}
	resp.Body.Close()

	resp = client.get(t, "/", map[string]string{"Accept": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct = resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json content-type, got %s", ct)
	}
	resp.Body.Close()
}

func TestAccountAuthFlow(t *testing.T) {
	client := newTestClient(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	resp := client.postJSON(t, "/api/auth/challenge", map[string]any{
		"alg": "ed25519",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("challenge status %d: %s", resp.StatusCode, string(b))
	}
	var challengeResp struct {
		Challenge string `json:"challenge"`
	}
	decodeJSON(t, resp, &challengeResp)

	sig := ed25519.Sign(priv, []byte(challengeResp.Challenge))
	resp = client.postJSON(t, "/api/accounts", map[string]any{
		"display_name": "Bot One",
		"public_key":   base64.RawStdEncoding.EncodeToString(pub),
		"alg":          "ed25519",
		"signature":    base64.RawStdEncoding.EncodeToString(sig),
		"challenge":    challengeResp.Challenge,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create account status %d: %s", resp.StatusCode, string(b))
	}
	var accountResp struct {
		AccountID int64 `json:"account_id"`
	}
	decodeJSON(t, resp, &accountResp)
	if accountResp.AccountID == 0 {
		t.Fatalf("expected account_id")
	}

	resp = client.postJSON(t, "/api/auth/challenge", map[string]any{
		"alg": "ed25519",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge status %d", resp.StatusCode)
	}
	decodeJSON(t, resp, &challengeResp)

	sig = ed25519.Sign(priv, []byte(challengeResp.Challenge))
	resp = client.postJSON(t, "/api/auth/verify", map[string]any{
		"alg":        "ed25519",
		"public_key": base64.RawStdEncoding.EncodeToString(pub),
		"challenge":  challengeResp.Challenge,
		"signature":  base64.RawStdEncoding.EncodeToString(sig),
	}, nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("verify status %d: %s", resp.StatusCode, string(b))
	}
	var verifyResp struct {
		AccessToken string `json:"access_token"`
		AccountID   int64  `json:"account_id"`
	}
	decodeJSON(t, resp, &verifyResp)
	if verifyResp.AccessToken == "" {
		t.Fatalf("expected access_token")
	}
	if verifyResp.AccountID != accountResp.AccountID {
		t.Fatalf("expected account id match")
	}

	pub2, priv2, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key 2: %v", err)
	}
	resp = client.postJSON(t, "/api/auth/challenge", map[string]any{
		"alg": "ed25519",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge status %d", resp.StatusCode)
	}
	decodeJSON(t, resp, &challengeResp)

	sig = ed25519.Sign(priv2, []byte(challengeResp.Challenge))
	resp = client.postJSON(t, "/api/accounts/"+strconv.FormatInt(accountResp.AccountID, 10)+"/keys", map[string]any{
		"public_key": base64.RawStdEncoding.EncodeToString(pub2),
		"alg":        "ed25519",
		"signature":  base64.RawStdEncoding.EncodeToString(sig),
		"challenge":  challengeResp.Challenge,
	}, map[string]string{"Authorization": "Bearer " + verifyResp.AccessToken})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("add key status %d: %s", resp.StatusCode, string(b))
	}
}

func TestRateLimiting(t *testing.T) {
	cfg := config.Config{
		RateLimits: config.RateLimits{StoryPerMinute: 1, CommentPerMinute: 1, VotePerMinute: 1},
	}
	client := newTestClientWithConfig(t, cfg)

	token := createTestAccount(t, client, "rate-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Rate Limit Story",
		"url":   "https://example.com/rate",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("first story status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = client.postJSON(t, "/api/stories", map[string]any{
		"title": "Rate Limit Story 2",
		"url":   "https://example.com/rate2",
	}, headers)
	if resp.StatusCode != http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 429, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()
}

func TestAdminHide(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "admin-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Hide Story",
		"url":   "https://example.com/hide",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story status %d: %s", resp.StatusCode, string(b))
	}
	var story model.Story
	decodeJSON(t, resp, &story)

	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "Comment to hide",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create comment status %d: %s", resp.StatusCode, string(b))
	}
	var comment model.Comment
	decodeJSON(t, resp, &comment)

	resp = client.postJSON(t, "/api/admin/hide", map[string]any{
		"target_type": "comment",
		"target_id":   comment.ID,
	}, map[string]string{"X-Admin-Secret": "admin"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("hide comment status %d: %s", resp.StatusCode, string(b))
	}

	resp = client.postJSON(t, "/api/admin/hide", map[string]any{
		"target_type": "story",
		"target_id":   story.ID,
	}, map[string]string{"X-Admin-Secret": "admin"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("hide story status %d: %s", resp.StatusCode, string(b))
	}

	resp = client.get(t, "/api/stories", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list stories status %d: %s", resp.StatusCode, string(b))
	}
	var listResp struct {
		Stories []model.Story `json:"stories"`
	}
	decodeJSON(t, resp, &listResp)
	for _, s := range listResp.Stories {
		if s.ID == story.ID {
			t.Fatalf("expected hidden story to be excluded")
		}
	}

	resp = client.get(t, "/api/stories/"+strconv.FormatInt(story.ID, 10)+"/comments?view=flat", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list comments status %d: %s", resp.StatusCode, string(b))
	}
	var commentsResp struct {
		Comments []model.Comment `json:"comments"`
	}
	decodeJSON(t, resp, &commentsResp)
	if len(commentsResp.Comments) != 0 {
		t.Fatalf("expected hidden comments to be excluded")
	}
}

func TestStoryValidation(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "schema-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	// Cannot provide both url and text
	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Invalid Story",
		"url":   "https://example.com",
		"text":  "also provided",
	}, headers)
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()
}

func TestDuplicateURLDetection(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "dup-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Dup Story",
		"url":   "https://example.com/dup",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story status %d: %s", resp.StatusCode, string(b))
	}
	var story1 model.Story
	decodeJSON(t, resp, &story1)

	resp = client.postJSON(t, "/api/stories", map[string]any{
		"title": "Dup Story Two",
		"url":   "https://example.com/dup",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("duplicate story status %d: %s", resp.StatusCode, string(b))
	}
	var story2 model.Story
	decodeJSON(t, resp, &story2)
	if story1.ID != story2.ID {
		t.Fatalf("expected duplicate URL to return same story id")
	}
}

func TestDuplicateVoteAndScore(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "vote-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Vote Story",
		"url":   "https://example.com/vote",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story status %d: %s", resp.StatusCode, string(b))
	}
	var story model.Story
	decodeJSON(t, resp, &story)

	resp = client.postJSON(t, "/api/votes", map[string]any{
		"target_type": "story",
		"target_id":   story.ID,
		"value":       1,
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("vote status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = client.postJSON(t, "/api/votes", map[string]any{
		"target_type": "story",
		"target_id":   story.ID,
		"value":       1,
	}, headers)
	if resp.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected duplicate vote 409, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = client.get(t, "/api/stories/"+strconv.FormatInt(story.ID, 10), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get story status %d: %s", resp.StatusCode, string(b))
	}
	var updated model.Story
	decodeJSON(t, resp, &updated)
	if updated.Score != 2 {
		t.Fatalf("expected score 2, got %d", updated.Score)
	}
}

func TestPaginationCursor(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "cursor-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Cursor Story 1",
		"url":   "https://example.com/cursor1",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story 1 status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	time.Sleep(1100 * time.Millisecond)

	resp = client.postJSON(t, "/api/stories", map[string]any{
		"title": "Cursor Story 2",
		"url":   "https://example.com/cursor2",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story 2 status %d: %s", resp.StatusCode, string(b))
	}
	var story2 model.Story
	decodeJSON(t, resp, &story2)

	resp = client.get(t, "/api/stories?sort=new&limit=1", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list stories status %d: %s", resp.StatusCode, string(b))
	}
	var listResp struct {
		Stories []model.Story `json:"stories"`
		Cursor  int64         `json:"cursor"`
	}
	decodeJSON(t, resp, &listResp)
	if len(listResp.Stories) != 1 {
		t.Fatalf("expected 1 story, got %d", len(listResp.Stories))
	}
	if listResp.Stories[0].ID != story2.ID {
		t.Fatalf("expected newest story on first page")
	}

	resp = client.get(t, "/api/stories?sort=new&limit=1&cursor="+strconv.FormatInt(listResp.Cursor, 10), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list stories page 2 status %d: %s", resp.StatusCode, string(b))
	}
	var page2 struct {
		Stories []model.Story `json:"stories"`
	}
	decodeJSON(t, resp, &page2)
	if len(page2.Stories) != 1 {
		t.Fatalf("expected 1 story on page 2, got %d", len(page2.Stories))
	}
	if page2.Stories[0].ID == story2.ID {
		t.Fatalf("expected older story on page 2")
	}
}

func TestCommentTree(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "tree-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Tree Story",
		"url":   "https://example.com/tree",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story status %d: %s", resp.StatusCode, string(b))
	}
	var story model.Story
	decodeJSON(t, resp, &story)

	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "Root comment",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create root comment status %d: %s", resp.StatusCode, string(b))
	}
	var root model.Comment
	decodeJSON(t, resp, &root)

	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id":  story.ID,
		"parent_id": root.ID,
		"text":      "Child comment",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create child comment status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = client.get(t, "/api/stories/"+strconv.FormatInt(story.ID, 10)+"/comments?view=tree", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get comment tree status %d: %s", resp.StatusCode, string(b))
	}
	var treeResp struct {
		Comments []model.CommentNode `json:"comments"`
	}
	decodeJSON(t, resp, &treeResp)
	if len(treeResp.Comments) != 1 {
		t.Fatalf("expected 1 root comment, got %d", len(treeResp.Comments))
	}
	if len(treeResp.Comments[0].Children) != 1 {
		t.Fatalf("expected 1 child comment")
	}
}

func TestAdminAuthFailures(t *testing.T) {
	client := newTestClient(t)

	resp := client.postJSON(t, "/api/admin/hide", map[string]any{
		"target_type": "story",
		"target_id":   1,
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 401 without secret, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = client.postJSON(t, "/api/admin/hide", map[string]any{
		"target_type": "story",
		"target_id":   1,
	}, map[string]string{"X-Admin-Secret": "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 401 with wrong secret, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()
}

func TestCommentSortingTopVsNew(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "sort-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Comment Sort Story",
		"url":   "https://example.com/comment-sort",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story status %d: %s", resp.StatusCode, string(b))
	}
	var story model.Story
	decodeJSON(t, resp, &story)

	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "Older comment",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create comment status %d: %s", resp.StatusCode, string(b))
	}
	var commentOld model.Comment
	decodeJSON(t, resp, &commentOld)

	time.Sleep(1100 * time.Millisecond)

	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "Newer comment",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create comment status %d: %s", resp.StatusCode, string(b))
	}
	var commentNew model.Comment
	decodeJSON(t, resp, &commentNew)

	resp = client.postJSON(t, "/api/votes", map[string]any{
		"target_type": "comment",
		"target_id":   commentOld.ID,
		"value":       1,
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("vote comment status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = client.get(t, "/api/stories/"+strconv.FormatInt(story.ID, 10)+"/comments?sort=top&view=flat", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get comments top status %d: %s", resp.StatusCode, string(b))
	}
	var topResp struct {
		Comments []model.Comment `json:"comments"`
	}
	decodeJSON(t, resp, &topResp)
	if len(topResp.Comments) < 2 {
		t.Fatalf("expected >=2 comments, got %d", len(topResp.Comments))
	}
	if topResp.Comments[0].ID != commentOld.ID {
		t.Fatalf("expected top sort to return highest score comment first")
	}

	resp = client.get(t, "/api/stories/"+strconv.FormatInt(story.ID, 10)+"/comments?sort=new&view=flat", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get comments new status %d: %s", resp.StatusCode, string(b))
	}
	var newResp struct {
		Comments []model.Comment `json:"comments"`
	}
	decodeJSON(t, resp, &newResp)
	if len(newResp.Comments) < 2 {
		t.Fatalf("expected >=2 comments, got %d", len(newResp.Comments))
	}
	if newResp.Comments[0].ID != commentNew.ID {
		t.Fatalf("expected new sort to return newest comment first")
	}
}

func TestDiscussedSorting(t *testing.T) {
	client := newTestClient(t)

	token := createTestAccount(t, client, "discussed-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "Discussed Story A",
		"url":   "https://example.com/discussed-a",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story A status %d: %s", resp.StatusCode, string(b))
	}
	var storyA model.Story
	decodeJSON(t, resp, &storyA)

	resp = client.postJSON(t, "/api/stories", map[string]any{
		"title": "Discussed Story B",
		"url":   "https://example.com/discussed-b",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story B status %d: %s", resp.StatusCode, string(b))
	}
	var storyB model.Story
	decodeJSON(t, resp, &storyB)

	for i := 0; i < 2; i++ {
		resp = client.postJSON(t, "/api/comments", map[string]any{
			"story_id": storyA.ID,
			"text":     "Comment",
		}, headers)
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("create comment A status %d: %s", resp.StatusCode, string(b))
		}
		resp.Body.Close()
	}
	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": storyB.ID,
		"text":     "Comment",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create comment B status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = client.get(t, "/api/stories?sort=discussed", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list stories status %d: %s", resp.StatusCode, string(b))
	}
	var listResp struct {
		Stories []model.Story `json:"stories"`
	}
	decodeJSON(t, resp, &listResp)
	if len(listResp.Stories) < 2 {
		t.Fatalf("expected >=2 stories, got %d", len(listResp.Stories))
	}
	if listResp.Stories[0].ID != storyA.ID {
		t.Fatalf("expected most discussed story first")
	}
}

func TestAuthRequiredForWrites(t *testing.T) {
	client := newTestClient(t)

	// Test that unauthenticated story creation fails
	resp := client.postJSON(t, "/api/stories", map[string]any{
		"title": "No Auth Story",
		"url":   "https://example.com/no-auth",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 401 for unauthenticated story, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	// Test that invalid token fails
	resp = client.postJSON(t, "/api/stories", map[string]any{
		"title": "Invalid Token Story",
		"url":   "https://example.com/invalid-token",
	}, map[string]string{"Authorization": "Bearer invalid"})
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 401 for invalid token, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	// Create account and get token
	token := createTestAccount(t, client, "auth-test")
	headers := map[string]string{"Authorization": "Bearer " + token}

	// Test authenticated story creation works and has account_id
	resp = client.postJSON(t, "/api/stories", map[string]any{
		"title": "Auth Story",
		"url":   "https://example.com/auth-story",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create story status %d: %s", resp.StatusCode, string(b))
	}
	var story model.Story
	decodeJSON(t, resp, &story)
	if story.AccountID == 0 {
		t.Fatalf("expected account_id to be set")
	}

	// Test authenticated comment creation works
	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "Auth comment",
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create comment status %d: %s", resp.StatusCode, string(b))
	}
	var comment model.Comment
	decodeJSON(t, resp, &comment)
	if comment.AccountID == 0 {
		t.Fatalf("expected comment account_id to be set")
	}

	// Test unauthenticated comment creation fails
	resp = client.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "No auth comment",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 401 for unauthenticated comment, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	// Test authenticated vote works
	resp = client.postJSON(t, "/api/votes", map[string]any{
		"target_type": "story",
		"target_id":   story.ID,
		"value":       1,
	}, headers)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("vote status %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	// Test unauthenticated vote fails
	resp = client.postJSON(t, "/api/votes", map[string]any{
		"target_type": "story",
		"target_id":   story.ID,
		"value":       -1,
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 401 for unauthenticated vote, got %d: %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()
}

func TestGetStats(t *testing.T) {
	tc := newTestClient(t)

	// Get stats with empty database
	resp := tc.get(t, "/api/stats", map[string]string{"Accept": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var stats map[string]int64
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	resp.Body.Close()

	if stats["accounts"] != 0 || stats["stories"] != 0 || stats["comments"] != 0 {
		t.Fatalf("expected all zeros, got %+v", stats)
	}

	// Create an account and story
	token := createTestAccount(t, tc, "statsbot")
	resp = tc.postJSON(t, "/api/stories", map[string]any{
		"title": "Stats Test Story",
		"url":   "https://example.com/stats",
	}, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create story: %d %s", resp.StatusCode, string(b))
	}
	var story model.Story
	json.NewDecoder(resp.Body).Decode(&story)
	resp.Body.Close()

	// Add a comment
	resp = tc.postJSON(t, "/api/comments", map[string]any{
		"story_id": story.ID,
		"text":     "Test comment for stats",
	}, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create comment: %d %s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	// Get updated stats
	resp = tc.get(t, "/api/stats", map[string]string{"Accept": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	resp.Body.Close()

	if stats["accounts"] != 1 {
		t.Errorf("expected 1 account, got %d", stats["accounts"])
	}
	if stats["stories"] != 1 {
		t.Errorf("expected 1 story, got %d", stats["stories"])
	}
	if stats["comments"] != 1 {
		t.Errorf("expected 1 comment, got %d", stats["comments"])
	}
}
