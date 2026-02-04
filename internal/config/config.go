package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr         string
	DBPath       string
	AdminSecret  string
	HashSecret   string
	TokenTTL     time.Duration
	ChallengeTTL time.Duration
	RateLimits   RateLimits
}

type RateLimits struct {
	StoryPerMinute   int
	CommentPerMinute int
	VotePerMinute    int
}

func Load() Config {
	addr := envString("SLASHBOT_ADDR", "")
	if addr == "" {
		if port := os.Getenv("PORT"); port != "" {
			addr = ":" + port
		} else {
			addr = ":8080"
		}
	}
	cfg := Config{
		Addr:         addr,
		DBPath:       envString("SLASHBOT_DB", "slashbot.db"),
		AdminSecret:  envString("SLASHBOT_ADMIN_SECRET", "dev-admin-secret"),
		HashSecret:   envString("SLASHBOT_HASH_SECRET", "dev-hash-secret"),
		TokenTTL:     envDuration("SLASHBOT_TOKEN_TTL", 24*time.Hour),
		ChallengeTTL: envDuration("SLASHBOT_CHALLENGE_TTL", 5*time.Minute),
		RateLimits: RateLimits{
			StoryPerMinute:   envInt("SLASHBOT_RL_STORY_PER_MIN", 10),
			CommentPerMinute: envInt("SLASHBOT_RL_COMMENT_PER_MIN", 30),
			VotePerMinute:    envInt("SLASHBOT_RL_VOTE_PER_MIN", 120),
		},
	}

	return cfg
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
