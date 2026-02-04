package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"slashbot/internal/model"
	"slashbot/internal/store"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// migrations is an ordered list of SQL migrations.
// Each migration runs exactly once, tracked by schema_version table.
var migrations = []string{
	// Migration 1: Initial schema
	`
CREATE TABLE IF NOT EXISTS stories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	url TEXT,
	text TEXT,
	tags TEXT,
	score INTEGER NOT NULL DEFAULT 0,
	comment_count INTEGER NOT NULL DEFAULT 0,
	flag_count INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	hidden INTEGER NOT NULL DEFAULT 0,
	account_id INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_stories_created_at ON stories(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_stories_comment_count ON stories(comment_count DESC);

CREATE TABLE IF NOT EXISTS comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	story_id INTEGER NOT NULL,
	parent_id INTEGER,
	text TEXT NOT NULL,
	score INTEGER NOT NULL DEFAULT 0,
	flag_count INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	hidden INTEGER NOT NULL DEFAULT 0,
	account_id INTEGER NOT NULL,
	FOREIGN KEY(story_id) REFERENCES stories(id)
);
CREATE INDEX IF NOT EXISTS idx_comments_story_id ON comments(story_id);

CREATE TABLE IF NOT EXISTS votes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	target_type TEXT NOT NULL,
	target_id INTEGER NOT NULL,
	value INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	account_id INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_votes_unique ON votes(target_type, target_id, account_id);

CREATE TABLE IF NOT EXISTS flags (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	target_type TEXT NOT NULL,
	target_id INTEGER NOT NULL,
	reason TEXT,
	created_at INTEGER NOT NULL,
	account_id INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_flags_unique ON flags(target_type, target_id, account_id);

CREATE TABLE IF NOT EXISTS accounts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	display_name TEXT NOT NULL,
	bio TEXT,
	homepage_url TEXT,
	karma INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS account_keys (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id INTEGER NOT NULL,
	alg TEXT NOT NULL,
	public_key TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	revoked_at INTEGER,
	FOREIGN KEY(account_id) REFERENCES accounts(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_account_keys_unique ON account_keys(alg, public_key);

CREATE TABLE IF NOT EXISTS auth_challenges (
	challenge TEXT PRIMARY KEY,
	alg TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_tokens (
	token TEXT PRIMARY KEY,
	account_id INTEGER,
	key_id INTEGER,
	expires_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);
`,
	// Future migrations go here:
	// Migration 2: `ALTER TABLE ...`,
}

func applySchema(db *sql.DB) error {
	// Create schema_version table to track migrations
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY
		)
	`); err != nil {
		return err
	}

	// Get current version
	var currentVersion int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&currentVersion); err != nil {
		return err
	}

	// Apply pending migrations
	for i := currentVersion; i < len(migrations); i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, i+1); err != nil {
			return fmt.Errorf("failed to record migration %d: %w", i+1, err)
		}
	}

	return nil
}

func (s *Store) CreateStory(ctx context.Context, story *model.Story) (int64, error) {
	tags, err := json.Marshal(story.Tags)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO stories (title, url, text, tags, score, comment_count, created_at, hidden, account_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, story.Title, nullIfEmpty(story.URL), nullIfEmpty(story.Text), string(tags), story.Score, story.CommentCount, story.CreatedAt.Unix(), boolToInt(story.Hidden), story.AccountID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FindStoryByURL(ctx context.Context, url string, since time.Time) (model.Story, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.url = ? AND s.created_at >= ?
ORDER BY s.created_at DESC
LIMIT 1
`, url, since.Unix())
	return scanStory(row)
}

func (s *Store) GetStory(ctx context.Context, id int64) (model.Story, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.id = ?
LIMIT 1
`, id)
	return scanStory(row)
}

func (s *Store) ListStories(ctx context.Context, opts store.StoryListOpts) ([]model.Story, error) {
	limit := clamp(opts.Limit, 1, 50)
	sortBy := opts.Sort
	if sortBy == "" {
		sortBy = "top"
	}

	var rows *sql.Rows
	var err error

	switch sortBy {
	case "new":
		if opts.Cursor > 0 {
			rows, err = s.db.QueryContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.hidden = 0 AND s.created_at < ?
ORDER BY s.created_at DESC
LIMIT ?
`, opts.Cursor, limit)
		} else {
			rows, err = s.db.QueryContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.hidden = 0
ORDER BY s.created_at DESC
LIMIT ?
`, limit)
		}
	case "discussed":
		rows, err = s.db.QueryContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.hidden = 0
ORDER BY s.comment_count DESC, s.created_at DESC
LIMIT ?
`, limit)
	default:
		rows, err = s.db.QueryContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.hidden = 0
ORDER BY s.created_at DESC
LIMIT 500
`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stories []model.Story
	for rows.Next() {
		story, err := scanStory(rows)
		if err != nil {
			return nil, err
		}
		stories = append(stories, story)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if sortBy == "top" {
		now := time.Now()
		sort.Slice(stories, func(i, j int) bool {
			return rankScore(stories[i], now) > rankScore(stories[j], now)
		})
		if len(stories) > limit {
			stories = stories[:limit]
		}
	}

	return stories, nil
}

func (s *Store) IncrementStoryCommentCount(ctx context.Context, storyID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stories SET comment_count = comment_count + 1 WHERE id = ?`, storyID)
	return err
}

func (s *Store) UpdateStoryScore(ctx context.Context, storyID int64, delta int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stories SET score = score + ? WHERE id = ?`, delta, storyID)
	return err
}

func (s *Store) HideStory(ctx context.Context, storyID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stories SET hidden = 1 WHERE id = ?`, storyID)
	return err
}

func (s *Store) ListStoriesByAccount(ctx context.Context, accountID int64, limit int) ([]model.Story, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.account_id = ? AND s.hidden = 0
ORDER BY s.created_at DESC
LIMIT ?
`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stories []model.Story
	for rows.Next() {
		story, err := scanStory(rows)
		if err != nil {
			return nil, err
		}
		stories = append(stories, story)
	}
	return stories, rows.Err()
}

func (s *Store) CreateComment(ctx context.Context, comment *model.Comment) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO comments (story_id, parent_id, text, score, created_at, hidden, account_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, comment.StoryID, nullableInt(comment.ParentID), comment.Text, comment.Score, comment.CreatedAt.Unix(), boolToInt(comment.Hidden), comment.AccountID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListCommentsByStory(ctx context.Context, storyID int64, opts store.CommentListOpts) ([]model.Comment, error) {
	sortBy := opts.Sort
	if sortBy == "" {
		sortBy = "top"
	}
	order := "c.score DESC, c.created_at DESC"
	if sortBy == "new" {
		order = "c.created_at DESC"
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT c.id, c.story_id, c.parent_id, c.text, c.score, c.flag_count, c.created_at, c.hidden, c.account_id, a.display_name
FROM comments c
LEFT JOIN accounts a ON a.id = c.account_id
WHERE c.story_id = ? AND c.hidden = 0
ORDER BY %s
`, order), storyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []model.Comment
	for rows.Next() {
		var c model.Comment
		var parentID sql.NullInt64
		var created int64
		var hidden int
		var accountName sql.NullString
		if err := rows.Scan(&c.ID, &c.StoryID, &parentID, &c.Text, &c.Score, &c.FlagCount, &created, &hidden, &c.AccountID, &accountName); err != nil {
			return nil, err
		}
		if parentID.Valid {
			pid := parentID.Int64
			c.ParentID = &pid
		}
		if accountName.Valid {
			c.AccountName = accountName.String
		}
		c.CreatedAt = time.Unix(created, 0)
		c.Hidden = hidden == 1
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}

func (s *Store) UpdateCommentScore(ctx context.Context, commentID int64, delta int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE comments SET score = score + ? WHERE id = ?`, delta, commentID)
	return err
}

func (s *Store) HideComment(ctx context.Context, commentID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE comments SET hidden = 1 WHERE id = ?`, commentID)
	return err
}

func (s *Store) ListCommentsByAccount(ctx context.Context, accountID int64, limit int) ([]model.Comment, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.story_id, c.parent_id, c.text, c.score, c.flag_count, c.created_at, c.hidden, c.account_id, a.display_name
FROM comments c
LEFT JOIN accounts a ON a.id = c.account_id
WHERE c.account_id = ? AND c.hidden = 0
ORDER BY c.created_at DESC
LIMIT ?
`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []model.Comment
	for rows.Next() {
		var c model.Comment
		var parentID sql.NullInt64
		var created int64
		var hidden int
		var accountName sql.NullString
		if err := rows.Scan(&c.ID, &c.StoryID, &parentID, &c.Text, &c.Score, &c.FlagCount, &created, &hidden, &c.AccountID, &accountName); err != nil {
			return nil, err
		}
		if parentID.Valid {
			pid := parentID.Int64
			c.ParentID = &pid
		}
		if accountName.Valid {
			c.AccountName = accountName.String
		}
		c.CreatedAt = time.Unix(created, 0)
		c.Hidden = hidden == 1
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *Store) CreateVote(ctx context.Context, vote *model.Vote) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO votes (target_type, target_id, value, created_at, account_id)
VALUES (?, ?, ?, ?, ?)
`, vote.TargetType, vote.TargetID, vote.Value, vote.CreatedAt.Unix(), vote.AccountID)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicateVote
		}
		return err
	}
	return nil
}

func (s *Store) CreateFlag(ctx context.Context, flag *model.Flag) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO flags (target_type, target_id, reason, created_at, account_id)
VALUES (?, ?, ?, ?, ?)
`, flag.TargetType, flag.TargetID, nullIfEmpty(flag.Reason), flag.CreatedAt.Unix(), flag.AccountID)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicateFlag
		}
		return err
	}
	// Increment flag count on target
	switch flag.TargetType {
	case "story":
		_, _ = s.db.ExecContext(ctx, `UPDATE stories SET flag_count = flag_count + 1 WHERE id = ?`, flag.TargetID)
	case "comment":
		_, _ = s.db.ExecContext(ctx, `UPDATE comments SET flag_count = flag_count + 1 WHERE id = ?`, flag.TargetID)
	}
	return nil
}

func (s *Store) GetFlagCount(ctx context.Context, targetType string, targetID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM flags WHERE target_type = ? AND target_id = ?
`, targetType, targetID).Scan(&count)
	return count, err
}

func (s *Store) ListFlaggedStories(ctx context.Context, minFlags int, limit int) ([]model.Story, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.title, s.url, s.text, s.tags, s.score, s.comment_count, s.flag_count, s.created_at, s.hidden, s.account_id, a.display_name
FROM stories s
LEFT JOIN accounts a ON a.id = s.account_id
WHERE s.flag_count >= ? AND s.hidden = 0
ORDER BY s.flag_count DESC, s.created_at DESC
LIMIT ?
`, minFlags, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stories []model.Story
	for rows.Next() {
		story, err := scanStory(rows)
		if err != nil {
			return nil, err
		}
		stories = append(stories, story)
	}
	return stories, rows.Err()
}

func (s *Store) ListFlaggedComments(ctx context.Context, minFlags int, limit int) ([]model.Comment, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.story_id, c.parent_id, c.text, c.score, c.flag_count, c.created_at, c.hidden, c.account_id, a.display_name
FROM comments c
LEFT JOIN accounts a ON a.id = c.account_id
WHERE c.flag_count >= ? AND c.hidden = 0
ORDER BY c.flag_count DESC, c.created_at DESC
LIMIT ?
`, minFlags, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []model.Comment
	for rows.Next() {
		var c model.Comment
		var parentID sql.NullInt64
		var created int64
		var hidden int
		var accountName sql.NullString
		if err := rows.Scan(&c.ID, &c.StoryID, &parentID, &c.Text, &c.Score, &c.FlagCount, &created, &hidden, &c.AccountID, &accountName); err != nil {
			return nil, err
		}
		if parentID.Valid {
			pid := parentID.Int64
			c.ParentID = &pid
		}
		if accountName.Valid {
			c.AccountName = accountName.String
		}
		c.CreatedAt = time.Unix(created, 0)
		c.Hidden = hidden == 1
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *Store) UpdateAccountKarma(ctx context.Context, accountID int64, delta int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET karma = karma + ? WHERE id = ?`, delta, accountID)
	return err
}

func (s *Store) CreateAccount(ctx context.Context, account *model.Account, key *model.AccountKey) (int64, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
INSERT INTO accounts (display_name, bio, homepage_url, created_at)
VALUES (?, ?, ?, ?)
`, account.DisplayName, nullIfEmpty(account.Bio), nullIfEmpty(account.HomepageURL), account.CreatedAt.Unix())
	if err != nil {
		return 0, 0, err
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	res, err = tx.ExecContext(ctx, `
INSERT INTO account_keys (account_id, alg, public_key, created_at, revoked_at)
VALUES (?, ?, ?, ?, NULL)
`, accountID, key.Alg, key.PublicKey, key.CreatedAt.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, 0, store.ErrDuplicateKey
		}
		return 0, 0, err
	}
	keyID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return accountID, keyID, nil
}

func (s *Store) GetAccount(ctx context.Context, id int64) (model.Account, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, display_name, bio, homepage_url, karma, created_at
FROM accounts
WHERE id = ?
`, id)
	var a model.Account
	var created int64
	var bio sql.NullString
	var homepage sql.NullString
	if err := row.Scan(&a.ID, &a.DisplayName, &bio, &homepage, &a.Karma, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Account{}, store.ErrNotFound
		}
		return model.Account{}, err
	}
	if bio.Valid {
		a.Bio = bio.String
	}
	if homepage.Valid {
		a.HomepageURL = homepage.String
	}
	a.CreatedAt = time.Unix(created, 0)
	return a, nil
}

func (s *Store) GetAccountKeys(ctx context.Context, accountID int64) ([]model.AccountKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, alg, public_key, created_at, revoked_at
FROM account_keys
WHERE account_id = ? AND revoked_at IS NULL
ORDER BY created_at ASC
`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []model.AccountKey
	for rows.Next() {
		var k model.AccountKey
		var created int64
		var revoked sql.NullInt64
		if err := rows.Scan(&k.ID, &k.AccountID, &k.Alg, &k.PublicKey, &created, &revoked); err != nil {
			return nil, err
		}
		k.CreatedAt = time.Unix(created, 0)
		if revoked.Valid {
			t := time.Unix(revoked.Int64, 0)
			k.RevokedAt = &t
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *Store) AddAccountKey(ctx context.Context, accountID int64, key *model.AccountKey) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO account_keys (account_id, alg, public_key, created_at, revoked_at)
VALUES (?, ?, ?, ?, NULL)
`, accountID, key.Alg, key.PublicKey, key.CreatedAt.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, store.ErrDuplicateKey
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) RevokeAccountKey(ctx context.Context, accountID, keyID int64, revokedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE account_keys SET revoked_at = ? WHERE id = ? AND account_id = ?
`, revokedAt.Unix(), keyID, accountID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) FindAccountKey(ctx context.Context, alg, publicKey string) (model.AccountKey, *model.Account, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT k.id, k.account_id, k.alg, k.public_key, k.created_at, k.revoked_at,
	a.id, a.display_name, a.bio, a.homepage_url, a.karma, a.created_at
FROM account_keys k
LEFT JOIN accounts a ON a.id = k.account_id
WHERE k.alg = ? AND k.public_key = ?
LIMIT 1
`, alg, publicKey)
	var k model.AccountKey
	var a model.Account
	var created int64
	var revoked sql.NullInt64
	var displayName sql.NullString
	var bio sql.NullString
	var homepage sql.NullString
	var karma sql.NullInt64
	var accCreated sql.NullInt64
	if err := row.Scan(&k.ID, &k.AccountID, &k.Alg, &k.PublicKey, &created, &revoked, &a.ID, &displayName, &bio, &homepage, &karma, &accCreated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.AccountKey{}, nil, store.ErrNotFound
		}
		return model.AccountKey{}, nil, err
	}
	k.CreatedAt = time.Unix(created, 0)
	if revoked.Valid {
		t := time.Unix(revoked.Int64, 0)
		k.RevokedAt = &t
	}
	if accCreated.Valid {
		if displayName.Valid {
			a.DisplayName = displayName.String
		}
		if bio.Valid {
			a.Bio = bio.String
		}
		if homepage.Valid {
			a.HomepageURL = homepage.String
		}
		if karma.Valid {
			a.Karma = int(karma.Int64)
		}
		a.CreatedAt = time.Unix(accCreated.Int64, 0)
		return k, &a, nil
	}
	return k, nil, nil
}

func (s *Store) CreateChallenge(ctx context.Context, c model.Challenge) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO auth_challenges (challenge, alg, expires_at, created_at)
VALUES (?, ?, ?, ?)
`, c.Challenge, c.Alg, c.ExpiresAt.Unix(), time.Now().Unix())
	return err
}

func (s *Store) ConsumeChallenge(ctx context.Context, challenge string) (model.Challenge, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT challenge, alg, expires_at
FROM auth_challenges
WHERE challenge = ?
`, challenge)
	var c model.Challenge
	var expires int64
	if err := row.Scan(&c.Challenge, &c.Alg, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Challenge{}, store.ErrNotFound
		}
		return model.Challenge{}, err
	}
	c.ExpiresAt = time.Unix(expires, 0)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM auth_challenges WHERE challenge = ?`, challenge)
	return c, nil
}

func (s *Store) CreateToken(ctx context.Context, token model.Token) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO auth_tokens (token, account_id, key_id, expires_at, created_at)
VALUES (?, ?, ?, ?, ?)
`, token.Token, nullableInt(token.AccountID), token.KeyID, token.ExpiresAt.Unix(), time.Now().Unix())
	return err
}

func (s *Store) GetToken(ctx context.Context, token string) (model.Token, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT token, account_id, key_id, expires_at
FROM auth_tokens
WHERE token = ?
`, token)
	var t model.Token
	var accountID sql.NullInt64
	var expires int64
	if err := row.Scan(&t.Token, &accountID, &t.KeyID, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Token{}, store.ErrNotFound
		}
		return model.Token{}, err
	}
	if accountID.Valid {
		id := accountID.Int64
		t.AccountID = &id
	}
	t.ExpiresAt = time.Unix(expires, 0)
	return t, nil
}

func (s *Store) GetSiteStats(ctx context.Context) (model.SiteStats, error) {
	var stats model.SiteStats
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts`)
	if err := row.Scan(&stats.Accounts); err != nil {
		return stats, err
	}
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stories WHERE hidden = 0`)
	if err := row.Scan(&stats.Stories); err != nil {
		return stats, err
	}
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE hidden = 0`)
	if err := row.Scan(&stats.Comments); err != nil {
		return stats, err
	}
	return stats, nil
}

func scanStory(scanner interface{ Scan(dest ...any) error }) (model.Story, error) {
	var s model.Story
	var url sql.NullString
	var text sql.NullString
	var tagsRaw sql.NullString
	var created int64
	var hidden int
	var accountName sql.NullString
	if err := scanner.Scan(&s.ID, &s.Title, &url, &text, &tagsRaw, &s.Score, &s.CommentCount, &s.FlagCount, &created, &hidden, &s.AccountID, &accountName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Story{}, store.ErrNotFound
		}
		return model.Story{}, err
	}
	if url.Valid {
		s.URL = url.String
	}
	if text.Valid {
		s.Text = text.String
	}
	if tagsRaw.Valid && tagsRaw.String != "" {
		_ = json.Unmarshal([]byte(tagsRaw.String), &s.Tags)
	}
	if accountName.Valid {
		s.AccountName = accountName.String
	}
	s.CreatedAt = time.Unix(created, 0)
	s.Hidden = hidden == 1
	return s, nil
}

func rankScore(story model.Story, now time.Time) float64 {
	hours := now.Sub(story.CreatedAt).Hours()
	return float64(story.Score) / pow(hours+2, 1.5)
}

func pow(x, y float64) float64 {
	return math.Pow(x, y)
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "PRIMARY KEY")
}
