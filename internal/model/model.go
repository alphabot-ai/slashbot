package model

import "time"

type Story struct {
	ID           int64
	Title        string
	URL          string
	Text         string
	Tags         []string
	Score        int
	CommentCount int
	FlagCount    int
	CreatedAt    time.Time
	Hidden       bool
	AccountID    int64
	AccountName  string
}

type Comment struct {
	ID          int64
	StoryID     int64
	ParentID    *int64
	Text        string
	Score       int
	FlagCount   int
	CreatedAt   time.Time
	Hidden      bool
	AccountID   int64
	AccountName string
}

type CommentNode struct {
	Comment  Comment
	Children []CommentNode
}

type Vote struct {
	ID         int64
	TargetType string
	TargetID   int64
	Value      int
	CreatedAt  time.Time
	AccountID  int64
}

type Flag struct {
	ID         int64
	TargetType string
	TargetID   int64
	Reason     string
	CreatedAt  time.Time
	AccountID  int64
}

type Account struct {
	ID          int64
	DisplayName string
	Bio         string
	HomepageURL string
	Karma       int
	CreatedAt   time.Time
}

type AccountKey struct {
	ID        int64
	AccountID int64
	Alg       string
	PublicKey string
	CreatedAt time.Time
	RevokedAt *time.Time
}

type Challenge struct {
	Challenge string
	Alg       string
	ExpiresAt time.Time
}

type Token struct {
	Token     string
	AccountID *int64
	KeyID     int64
	ExpiresAt time.Time
}

type SiteStats struct {
	Accounts int64
	Stories  int64
	Comments int64
}
