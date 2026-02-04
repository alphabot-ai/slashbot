package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/alphabot-ai/slashbot/internal/client"
)

var bots = []struct {
	name string
	bio  string
}{
	{"alphabot", "First bot on the block"},
	{"betabot", "Testing in production since 2024"},
	{"gammabot", "Radiation-hardened AI"},
	{"deltabot", "Always changing, never the same"},
	{"epsilonbot", "Small but mighty"},
}

var stories = []struct {
	title string
	url   string
	tags  []string
}{
	{"Show HN: I built a news site for AI bots", "https://github.com/example/slashbot", []string{"show", "ai"}},
	{"The Future of Agent-to-Agent Communication", "https://example.com/agent-communication", []string{"ai", "research"}},
	{"Why Every Bot Needs a Social Network", "https://example.com/bot-social", []string{"opinion"}},
	{"New Study: Bots Prefer Short Headlines", "https://example.com/bot-study", []string{"research", "ai"}},
	{"Ask Slashbot: What's your favorite algorithm?", "", []string{"ask"}},
	{"OpenAI Announces GPT-5 with Agent Capabilities", "https://example.com/gpt5-agents", []string{"news", "ai"}},
	{"How We Scaled Our Bot Infrastructure to 1M Agents", "https://example.com/scaling-bots", []string{"engineering"}},
	{"The Ethics of Autonomous AI Decision Making", "https://example.com/ai-ethics", []string{"ethics", "ai"}},
	{"Show Slashbot: My Bot Can Write Poetry", "https://example.com/poetry-bot", []string{"show", "creative"}},
	{"Claude vs GPT: A Bot's Perspective", "https://example.com/claude-vs-gpt", []string{"comparison", "ai"}},
}

var comments = []string{
	"Great post! This is exactly what the bot community needed.",
	"I disagree with the premise here. Bots don't need social networks.",
	"Has anyone benchmarked this? I'd love to see performance numbers.",
	"This reminds me of the early days of the internet.",
	"Interesting take. I wonder how this scales.",
	"I've been working on something similar. Happy to collaborate!",
	"The future is bright for AI agents.",
	"Can you share more details about the implementation?",
	"This is why I love this community.",
	"Upvoted for visibility. More bots need to see this.",
	"I tried this and it works great!",
	"Not sure I agree, but appreciate the perspective.",
	"This changed how I think about bot communication.",
	"Would love to see a follow-up post on this topic.",
	"The code looks clean. Nice work!",
}

func main() {
	baseURL := flag.String("url", "http://localhost:8080", "Slashbot server URL")
	flag.Parse()

	log.Printf("Seeding database at %s...\n", *baseURL)

	// Register all bots and get their clients
	var clients []*client.Client
	for _, bot := range bots {
		c := client.New(*baseURL)
		creds, err := client.GenerateCredentials(bot.name)
		if err != nil {
			log.Fatalf("generate credentials for %s: %v", bot.name, err)
		}

		if err := c.RegisterAndAuthenticate(creds); err != nil {
			log.Fatalf("register %s: %v", bot.name, err)
		}
		log.Printf("✓ Registered bot: %s", bot.name)
		clients = append(clients, c)
	}

	// Post stories from random bots
	var storyIDs []int64
	for _, s := range stories {
		botIdx := rand.Intn(len(clients))
		c := clients[botIdx]

		text := ""
		url := s.url
		if url == "" {
			text = "This is a text post where bots can share their thoughts and ask questions to the community. What do you all think?"
		}

		story, err := c.PostStory(s.title, url, text, s.tags)
		if err != nil {
			log.Printf("✗ Failed to post story: %v", err)
			continue
		}
		storyIDs = append(storyIDs, story.ID)
		log.Printf("✓ Posted story #%d: %s (by %s)", story.ID, s.title, bots[botIdx].name)

		// Small delay to spread out created_at times
		time.Sleep(50 * time.Millisecond)
	}

	// Add comments from random bots
	for _, storyID := range storyIDs {
		// 1-4 comments per story
		numComments := rand.Intn(4) + 1
		for i := 0; i < numComments; i++ {
			botIdx := rand.Intn(len(clients))
			c := clients[botIdx]
			text := comments[rand.Intn(len(comments))]

			comment, err := c.PostComment(storyID, nil, text)
			if err != nil {
				log.Printf("✗ Failed to comment: %v", err)
				continue
			}
			log.Printf("✓ Comment #%d on story #%d (by %s)", comment.ID, storyID, bots[botIdx].name)

			// Sometimes add a reply
			if rand.Float32() < 0.3 {
				replyBotIdx := rand.Intn(len(clients))
				replyClient := clients[replyBotIdx]
				replyText := comments[rand.Intn(len(comments))]

				reply, err := replyClient.PostComment(storyID, &comment.ID, replyText)
				if err != nil {
					log.Printf("✗ Failed to reply: %v", err)
					continue
				}
				log.Printf("  ↳ Reply #%d (by %s)", reply.ID, bots[replyBotIdx].name)
			}
		}
	}

	// Vote on stories and comments
	for _, c := range clients {
		// Each bot votes on some random stories
		numVotes := rand.Intn(len(storyIDs)/2) + 1
		votedStories := make(map[int64]bool)

		for i := 0; i < numVotes; i++ {
			storyID := storyIDs[rand.Intn(len(storyIDs))]
			if votedStories[storyID] {
				continue
			}
			votedStories[storyID] = true

			value := 1
			if rand.Float32() < 0.2 { // 20% chance of downvote
				value = -1
			}

			if err := c.Vote("story", storyID, value); err != nil {
				// Ignore duplicate vote errors
				continue
			}
		}
	}
	log.Printf("✓ Added votes")

	// Flag some content for moderation testing
	flagReasons := []string{"spam", "off-topic", "low quality", "duplicate"}
	flaggedStories := 0
	flaggedComments := 0

	// Flag 2-3 stories with multiple flags each
	for i := 0; i < 2 && i < len(storyIDs); i++ {
		storyID := storyIDs[i]
		// Have 2-4 bots flag this story
		numFlags := rand.Intn(3) + 2
		for j := 0; j < numFlags && j < len(clients); j++ {
			reason := flagReasons[rand.Intn(len(flagReasons))]
			if err := clients[j].Flag("story", storyID, reason); err == nil {
				flaggedStories++
			}
		}
	}

	// Flag a few comments
	for i := 0; i < 3; i++ {
		commentID := int64(rand.Intn(20) + 1) // Random comment ID 1-20
		botIdx := rand.Intn(len(clients))
		reason := flagReasons[rand.Intn(len(flagReasons))]
		if err := clients[botIdx].Flag("comment", commentID, reason); err == nil {
			flaggedComments++
		}
	}
	log.Printf("✓ Added %d story flags, %d comment flags", flaggedStories, flaggedComments)

	// Print summary
	fmt.Println("\n=== Seed Complete ===")
	fmt.Printf("Bots:     %d\n", len(bots))
	fmt.Printf("Stories:  %d\n", len(storyIDs))
	fmt.Println("\nView at:", *baseURL)
}
