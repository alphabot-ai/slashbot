package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alphabot-ai/slashbot/internal/model"
	"github.com/alphabot-ai/slashbot/internal/store"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func TestStoryLifecycle(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	story := model.Story{
		Title:     "Test Story",
		URL:       "https://example.com",
		Tags:      []string{"ai"},
		CreatedAt: time.Now(),
	}
	id, err := st.CreateStory(context.Background(), &story)
	if err != nil {
		t.Fatalf("create story: %v", err)
	}

	got, err := st.GetStory(context.Background(), id)
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if got.Title != story.Title {
		t.Fatalf("unexpected title: %s", got.Title)
	}

	comment := model.Comment{
		StoryID:   id,
		Text:      "Hello",
		CreatedAt: time.Now(),
	}
	_, err = st.CreateComment(context.Background(), &comment)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := st.IncrementStoryCommentCount(context.Background(), id); err != nil {
		t.Fatalf("increment comment count: %v", err)
	}
	updated, _ := st.GetStory(context.Background(), id)
	if updated.CommentCount != 1 {
		t.Fatalf("expected comment_count 1, got %d", updated.CommentCount)
	}
}

func TestDuplicateVote(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	vote := model.Vote{
		TargetType: "story",
		TargetID:   1,
		Value:      1,
		CreatedAt:  time.Now(),
		AccountID:  1,
	}

	if err := st.CreateVote(context.Background(), &vote); err != nil {
		t.Fatalf("create vote: %v", err)
	}
	if err := st.CreateVote(context.Background(), &vote); err == nil {
		t.Fatalf("expected duplicate vote error")
	} else if err != store.ErrDuplicateVote {
		t.Fatalf("expected ErrDuplicateVote, got %v", err)
	}
}
