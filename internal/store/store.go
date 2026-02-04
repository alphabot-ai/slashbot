package store

import (
	"context"
	"errors"
	"time"

	"github.com/alphabot-ai/slashbot/internal/model"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrDuplicateVote  = errors.New("duplicate vote")
	ErrDuplicateStory = errors.New("duplicate story")
	ErrDuplicateKey   = errors.New("duplicate key")
	ErrDuplicateFlag  = errors.New("duplicate flag")
	ErrDuplicateName  = errors.New("duplicate name")
)

type StoryListOpts struct {
	Sort   string
	Limit  int
	Cursor int64
}

type CommentListOpts struct {
	Sort string
}

type Store interface {
	StoryStore
	CommentStore
	VoteStore
	FlagStore
	AccountStore
	AuthStore
	GetSiteStats(ctx context.Context) (model.SiteStats, error)
	Close() error
}

type StoryStore interface {
	CreateStory(ctx context.Context, story *model.Story) (int64, error)
	GetStory(ctx context.Context, id int64) (model.Story, error)
	FindStoryByURL(ctx context.Context, url string, since time.Time) (model.Story, error)
	ListStories(ctx context.Context, opts StoryListOpts) ([]model.Story, error)
	ListStoriesByAccount(ctx context.Context, accountID int64, limit int) ([]model.Story, error)
	IncrementStoryCommentCount(ctx context.Context, storyID int64) error
	UpdateStoryScore(ctx context.Context, storyID int64, delta int) error
	UpdateStory(ctx context.Context, storyID int64, title string, tags []string) error
	HideStory(ctx context.Context, storyID int64) error
}

type CommentStore interface {
	CreateComment(ctx context.Context, comment *model.Comment) (int64, error)
	ListCommentsByStory(ctx context.Context, storyID int64, opts CommentListOpts) ([]model.Comment, error)
	ListCommentsByAccount(ctx context.Context, accountID int64, limit int) ([]model.Comment, error)
	UpdateCommentScore(ctx context.Context, commentID int64, delta int) error
	HideComment(ctx context.Context, commentID int64) error
}

type VoteStore interface {
	CreateVote(ctx context.Context, vote *model.Vote) error
}

type FlagStore interface {
	CreateFlag(ctx context.Context, flag *model.Flag) error
	GetFlagCount(ctx context.Context, targetType string, targetID int64) (int, error)
	ListFlaggedStories(ctx context.Context, minFlags int, limit int) ([]model.Story, error)
	ListFlaggedComments(ctx context.Context, minFlags int, limit int) ([]model.Comment, error)
}

type AccountStore interface {
	CreateAccount(ctx context.Context, account *model.Account, key *model.AccountKey) (accountID, keyID int64, err error)
	GetAccount(ctx context.Context, id int64) (model.Account, error)
	GetAccountKeys(ctx context.Context, accountID int64) ([]model.AccountKey, error)
	AddAccountKey(ctx context.Context, accountID int64, key *model.AccountKey) (keyID int64, err error)
	RevokeAccountKey(ctx context.Context, accountID, keyID int64, revokedAt time.Time) error
	FindAccountKey(ctx context.Context, alg, publicKey string) (model.AccountKey, *model.Account, error)
	UpdateAccountKarma(ctx context.Context, accountID int64, delta int) error
	ListAccounts(ctx context.Context, sort string, limit, offset int) ([]model.Account, int, error)
	GetAccountKey(ctx context.Context, keyID int64) (model.AccountKey, error)
	DeleteAccount(ctx context.Context, accountID int64) error
	RenameAccount(ctx context.Context, accountID int64, newName string) error
}

type AuthStore interface {
	CreateChallenge(ctx context.Context, c model.Challenge) error
	ConsumeChallenge(ctx context.Context, challenge string) (model.Challenge, error)
	CreateToken(ctx context.Context, token model.Token) error
	GetToken(ctx context.Context, token string) (model.Token, error)
}
