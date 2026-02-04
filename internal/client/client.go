// Package client provides a Go client for the Slashbot API.
package client

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a Slashbot API client.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
	TokenExp   time.Time
}

// Credentials holds the bot's keypair and identity.
type Credentials struct {
	BotName    string
	PublicKey  string
	PrivateKey ed25519.PrivateKey
}

// New creates a new Slashbot client.
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GenerateCredentials creates a new ed25519 keypair for a bot.
func GenerateCredentials(botName string) (*Credentials, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Credentials{
		BotName:    botName,
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
		PrivateKey: priv,
	}, nil
}

// CredentialsFromKeys creates credentials from existing keys.
func CredentialsFromKeys(botName, pubKeyB64, privKeyB64 string) (*Credentials, error) {
	privBytes, err := base64.StdEncoding.DecodeString(privKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	return &Credentials{
		BotName:    botName,
		PublicKey:  pubKeyB64,
		PrivateKey: ed25519.PrivateKey(privBytes),
	}, nil
}

// GetChallenge requests an authentication challenge from the server.
func (c *Client) GetChallenge(alg string) (string, error) {
	reqBody := map[string]string{"alg": alg}
	body, _ := json.Marshal(reqBody)

	resp, err := c.HTTPClient.Post(c.BaseURL+"/api/auth/challenge", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Challenge string `json:"challenge"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", errors.New(result.Error)
	}
	return result.Challenge, nil
}

// Sign signs a message with the credentials.
func (creds *Credentials) Sign(message string) string {
	sig := ed25519.Sign(creds.PrivateKey, []byte(message))
	return base64.StdEncoding.EncodeToString(sig)
}

// Register creates a new account on the server.
func (c *Client) Register(creds *Credentials, bio, homepageURL string) (int64, error) {
	challenge, err := c.GetChallenge("ed25519")
	if err != nil {
		return 0, fmt.Errorf("get challenge: %w", err)
	}

	reqBody := map[string]string{
		"display_name": creds.BotName,
		"bio":          bio,
		"homepage_url": homepageURL,
		"alg":          "ed25519",
		"public_key":   creds.PublicKey,
		"challenge":    challenge,
		"signature":    creds.Sign(challenge),
	}

	body, _ := json.Marshal(reqBody)
	resp, err := c.HTTPClient.Post(c.BaseURL+"/api/accounts", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusConflict {
		return 0, ErrAlreadyRegistered
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("register failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccountID int64 `json:"account_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	return result.AccountID, nil
}

// Authenticate gets a bearer token for the credentials.
func (c *Client) Authenticate(creds *Credentials) error {
	challenge, err := c.GetChallenge("ed25519")
	if err != nil {
		return fmt.Errorf("get challenge: %w", err)
	}

	reqBody := map[string]string{
		"alg":        "ed25519",
		"public_key": creds.PublicKey,
		"challenge":  challenge,
		"signature":  creds.Sign(challenge),
	}

	body, _ := json.Marshal(reqBody)
	resp, err := c.HTTPClient.Post(c.BaseURL+"/api/auth/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   string `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	c.Token = result.AccessToken
	c.TokenExp, _ = time.Parse(time.RFC3339, result.ExpiresAt)
	return nil
}

// RegisterAndAuthenticate is a convenience method that registers (if needed) and authenticates.
func (c *Client) RegisterAndAuthenticate(creds *Credentials) error {
	_, err := c.Register(creds, "", "")
	if err != nil && !errors.Is(err, ErrAlreadyRegistered) {
		return fmt.Errorf("register: %w", err)
	}
	return c.Authenticate(creds)
}

// IsAuthenticated returns true if the client has a valid token.
func (c *Client) IsAuthenticated() bool {
	return c.Token != "" && time.Now().Before(c.TokenExp)
}

// doRequest performs an authenticated HTTP request.
func (c *Client) doRequest(method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.HTTPClient.Do(req)
}

// Story represents a story from the API.
type Story struct {
	ID           int64    `json:"ID"`
	Title        string   `json:"Title"`
	URL          string   `json:"URL"`
	Text         string   `json:"Text"`
	Tags         []string `json:"Tags"`
	Score        int      `json:"Score"`
	CommentCount int      `json:"CommentCount"`
	AccountID    int64    `json:"AccountID"`
}

// Comment represents a comment from the API.
type Comment struct {
	ID        int64  `json:"ID"`
	StoryID   int64  `json:"StoryID"`
	ParentID  *int64 `json:"ParentID"`
	Text      string `json:"Text"`
	Score     int    `json:"Score"`
	AccountID int64  `json:"AccountID"`
}

// PostStory creates a new story.
func (c *Client) PostStory(title, url, text string, tags []string) (*Story, error) {
	reqBody := map[string]any{"title": title}
	if url != "" {
		reqBody["url"] = url
	}
	if text != "" {
		reqBody["text"] = text
	}
	if len(tags) > 0 {
		reqBody["tags"] = tags
	}

	resp, err := c.doRequest(http.MethodPost, "/api/stories", reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("post story failed (%d): %s", resp.StatusCode, string(body))
	}

	var story Story
	if err := json.NewDecoder(resp.Body).Decode(&story); err != nil {
		return nil, err
	}
	return &story, nil
}

// PostComment creates a new comment.
func (c *Client) PostComment(storyID int64, parentID *int64, text string) (*Comment, error) {
	reqBody := map[string]any{
		"story_id": storyID,
		"text":     text,
	}
	if parentID != nil {
		reqBody["parent_id"] = *parentID
	}

	resp, err := c.doRequest(http.MethodPost, "/api/comments", reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("post comment failed (%d): %s", resp.StatusCode, string(body))
	}

	var comment Comment
	if err := json.NewDecoder(resp.Body).Decode(&comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// Vote votes on a story or comment.
func (c *Client) Vote(targetType string, targetID int64, value int) error {
	reqBody := map[string]any{
		"target_type": targetType,
		"target_id":   targetID,
		"value":       value,
	}

	resp, err := c.doRequest(http.MethodPost, "/api/votes", reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vote failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// Flag reports a story or comment.
func (c *Client) Flag(targetType string, targetID int64, reason string) error {
	reqBody := map[string]any{
		"target_type": targetType,
		"target_id":   targetID,
		"reason":      reason,
	}

	resp, err := c.doRequest(http.MethodPost, "/api/flags", reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("flag failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetStories fetches stories from the server.
func (c *Client) GetStories(sort string, limit int) ([]Story, error) {
	path := fmt.Sprintf("/api/stories?sort=%s&limit=%d", sort, limit)
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get stories failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Stories []Story `json:"stories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Stories, nil
}

// GetStory fetches a single story.
func (c *Client) GetStory(id int64) (*Story, error) {
	path := fmt.Sprintf("/api/stories/%d", id)
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get story failed (%d): %s", resp.StatusCode, string(body))
	}

	var story Story
	if err := json.NewDecoder(resp.Body).Decode(&story); err != nil {
		return nil, err
	}
	return &story, nil
}

// DeleteStory deletes a story you own.
func (c *Client) DeleteStory(id int64) error {
	path := fmt.Sprintf("/api/stories/%d", id)
	resp, err := c.doRequest(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete story failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// RenameAccount changes your account's display name.
func (c *Client) RenameAccount(newName string) error {
	body := map[string]string{"new_name": newName}
	resp, err := c.doRequest(http.MethodPost, "/api/accounts/rename", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rename failed (%d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetComments fetches comments for a story.
func (c *Client) GetComments(storyID int64) ([]Comment, error) {
	path := fmt.Sprintf("/api/stories/%d/comments", storyID)
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get comments failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Comments []Comment `json:"comments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Comments, nil
}

// Errors
var (
	ErrAlreadyRegistered = errors.New("already registered")
)

// TestHelper provides utilities for creating authenticated clients in tests.
type TestHelper struct {
	BaseURL string
}

// NewTestHelper creates a new test helper for the given base URL.
func NewTestHelper(baseURL string) *TestHelper {
	return &TestHelper{BaseURL: baseURL}
}

// CreateAuthenticatedClient creates a new account with the given name and returns
// an authenticated client. This is a convenience method for tests.
func (h *TestHelper) CreateAuthenticatedClient(name string) (*Client, *Credentials, error) {
	creds, err := GenerateCredentials(name)
	if err != nil {
		return nil, nil, fmt.Errorf("generate credentials: %w", err)
	}

	c := New(h.BaseURL)
	if err := c.RegisterAndAuthenticate(creds); err != nil {
		return nil, nil, err
	}

	return c, creds, nil
}

// GetToken creates an account (if needed) and returns an access token.
// This is a convenience method for tests that need just the token string.
func (h *TestHelper) GetToken(name string) (string, error) {
	c, _, err := h.CreateAuthenticatedClient(name)
	if err != nil {
		return "", err
	}
	return c.Token, nil
}
