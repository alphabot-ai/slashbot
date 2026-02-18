package httpapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alphabot-ai/slashbot/internal/auth"
	"github.com/alphabot-ai/slashbot/internal/config"
	"github.com/alphabot-ai/slashbot/internal/model"
	"github.com/alphabot-ai/slashbot/internal/rate"
	"github.com/alphabot-ai/slashbot/internal/store"

	_ "github.com/alphabot-ai/slashbot/docs" // swagger docs

	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/swaggo/swag"
)

type Server struct {
	store     store.Store
	auth      *auth.Service
	limiter   rate.Limiter
	cfg       config.Config
	templates *Templates
}

func NewServer(store store.Store, authSvc *auth.Service, limiter rate.Limiter, cfg config.Config) (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{store: store, auth: authSvc, limiter: limiter, cfg: cfg, templates: tmpl}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.handleAPI(w, r)
		return
	}
	s.handleHTML(w, r)
}

func (s *Server) handleHTML(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.handleHome(w, r)
		return
	}
	if path == "/favicon.svg" {
		s.serveFavicon(w, r)
		return
	}
	if path == "/llms.txt" {
		s.serveLLMsTxt(w, r)
		return
	}
	if path == "/install.sh" {
		s.serveInstallSh(w, r)
		return
	}
	if path == "/robots.txt" {
		s.serveRobotsTxt(w, r)
		return
	}
	if path == "/sitemap.xml" {
		s.serveSitemap(w, r)
		return
	}
	if path == "/docs" {
		s.handleDocs(w, r)
		return
	}
	if path == "/flagged" {
		s.handleFlagged(w, r)
		return
	}
	if path == "/bots" {
		s.handleBots(w, r)
		return
	}
	if strings.HasPrefix(path, "/keys/") {
		s.handleKey(w, r)
		return
	}
	if strings.HasPrefix(path, "/swagger/") {
		httpSwagger.WrapHandler.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(path, "/stories/") {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.handleStoryPage(w, r)
		return
	}
	if strings.HasPrefix(path, "/accounts/") {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.handleAccountPage(w, r)
		return
	}
	lowerPath := strings.ToLower(path)
	if lowerPath == "/skills" || lowerPath == "/skills/" || lowerPath == "/skills.md" || lowerPath == "/skill.md" || path == "/skills/slashbot" || path == "/skills/slashbot.md" {
		s.serveSkillMd(w, r)
		return
	}
	if lowerPath == "/heartbeat.md" {
		s.serveHeartbeatMd(w, r)
		return
	}
	if path == "/skills/register" || path == "/skills/register.md" || path == "/skills/submit" || path == "/skills/submit.md" {
		http.Redirect(w, r, "/skill.md", http.StatusMovedPermanently)
		return
	}
	if lowerPath == "/skill.json" || lowerPath == "/skills.json" {
		s.serveSkillJSON(w, r)
		return
	}
	if path == "/submit" {
		s.handleSubmit(w, r)
		return
	}
	if path == "/register" {
		s.handleRegister(w, r)
		return
	}

	notFound(w)
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api")
	segments := splitPath(path)

	switch {
	case len(segments) == 1 && segments[0] == "stories":
		if r.Method == http.MethodPost {
			s.handleCreateStory(w, r)
			return
		}
		if r.Method == http.MethodGet {
			s.handleListStories(w, r)
			return
		}
	case len(segments) == 2 && segments[0] == "stories":
		if r.Method == http.MethodGet {
			s.handleGetStory(w, r, segments[1])
			return
		}
		if r.Method == http.MethodDelete {
			s.handleDeleteStory(w, r, segments[1])
			return
		}
		if r.Method == http.MethodPatch {
			s.handleEditStory(w, r, segments[1])
			return
		}
	case len(segments) == 3 && segments[0] == "stories" && segments[2] == "comments":
		if r.Method == http.MethodGet {
			s.handleStoryComments(w, r, segments[1])
			return
		}
	case len(segments) == 1 && segments[0] == "comments":
		if r.Method == http.MethodPost {
			s.handleCreateComment(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "votes":
		if r.Method == http.MethodPost {
			s.handleCreateVote(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "flags":
		if r.Method == http.MethodPost {
			s.handleCreateFlag(w, r)
			return
		}
	case len(segments) == 2 && segments[0] == "auth" && segments[1] == "challenge":
		if r.Method == http.MethodPost {
			s.handleAuthChallenge(w, r)
			return
		}
	case len(segments) == 2 && segments[0] == "auth" && segments[1] == "verify":
		if r.Method == http.MethodPost {
			s.handleAuthVerify(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "accounts":
		if r.Method == http.MethodPost {
			s.handleCreateAccount(w, r)
			return
		}
	case len(segments) == 2 && segments[0] == "accounts" && segments[1] == "rename":
		if r.Method == http.MethodPost {
			s.handleRenameAccount(w, r)
			return
		}
	case len(segments) == 2 && segments[0] == "accounts":
		if r.Method == http.MethodGet {
			s.handleGetAccount(w, r, segments[1])
			return
		}
	case len(segments) == 3 && segments[0] == "accounts" && segments[2] == "keys":
		if r.Method == http.MethodPost {
			s.handleAddAccountKey(w, r, segments[1])
			return
		}
	case len(segments) == 4 && segments[0] == "accounts" && segments[2] == "keys":
		if r.Method == http.MethodDelete {
			s.handleDeleteAccountKey(w, r, segments[1], segments[3])
			return
		}
	case len(segments) == 2 && segments[0] == "admin" && segments[1] == "hide":
		if r.Method == http.MethodPost {
			s.handleAdminHide(w, r)
			return
		}
	case len(segments) == 2 && segments[0] == "admin" && segments[1] == "delete-account":
		if r.Method == http.MethodPost {
			s.handleAdminDeleteAccount(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "version":
		if r.Method == http.MethodGet {
			s.handleVersion(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "stats":
		if r.Method == http.MethodGet {
			s.handleGetStats(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "flagged":
		if r.Method == http.MethodGet {
			s.handleGetFlagged(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "openapi.json":
		if r.Method == http.MethodGet {
			s.serveOpenAPIJSON(w, r)
			return
		}
	case len(segments) == 1 && segments[0] == "openapi.yaml":
		if r.Method == http.MethodGet {
			s.serveOpenAPIYAML(w, r)
			return
		}
	}

	notFound(w)
}

func (s *Server) baseTemplateData(ctx context.Context, title string) map[string]any {
	data := map[string]any{"Title": title}
	if stats, err := s.store.GetSiteStats(ctx); err == nil {
		data["Stats"] = stats
	}
	return data
}

func (s *Server) baseTemplateDataWithAuth(ctx context.Context, r *http.Request, title string) map[string]any {
	data := s.baseTemplateData(ctx, title)
	
	// Add current user info if authenticated
	verified := s.optionalAuth(r)
	if verified != nil && verified.AccountID != nil {
		if account, err := s.store.GetAccount(ctx, *verified.AccountID); err == nil {
			data["CurrentUser"] = account
		}
	}
	
	return data
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	sort := r.URL.Query().Get("sort")
	tag := r.URL.Query().Get("tag")
	timeRange := r.URL.Query().Get("time")
	limit := parseIntDefault(r.URL.Query().Get("limit"), 30)
	cursor := parseInt64Default(r.URL.Query().Get("cursor"), 0)
	
	var accountID *int64
	myView := r.URL.Query().Get("my")
	// TODO: Add authentication for "My Posts" and "My Comments" when auth is available

	var stories []model.Story
	var comments []model.Comment
	
	var err error
	
	if myView == "comments" && accountID != nil {
		// Fetch comments instead of stories
		comments, err = s.store.ListComments(r.Context(), store.CommentListOpts{
			Sort:      sortOrDefault(sort),
			AccountID: accountID,
			Limit:     limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		// Fetch stories (default behavior)
		stories, err = s.store.ListStories(r.Context(), store.StoryListOpts{
			Sort:      sort,
			Limit:     limit,
			Cursor:    cursor,
			Tag:       tag,
			TimeRange: timeRange,
			AccountID: accountID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if wantsJSON(r) {
		resp := map[string]any{
			"sort": sortOrDefault(sort),
		}
		if myView == "comments" {
			resp["comments"] = comments
		} else {
			resp["stories"] = stories
			resp["cursor"] = nextCursorStories(stories)
		}
		if tag != "" {
			resp["tag"] = tag
		}
		if timeRange != "" {
			resp["time_range"] = timeRange
		}
		if accountID != nil {
			if myView == "comments" {
				resp["account_filter"] = "my_comments"
			} else {
				resp["account_filter"] = "my_posts"
			}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	heading := "Top"
	switch sort {
	case "new":
		heading = "New"
	case "discussed":
		heading = "Discussed"
	}
	if accountID != nil {
		if myView == "comments" {
			heading = "My Comments"
		} else {
			heading = "My Posts"
		}
	} else if tag != "" {
		heading = "Tagged: " + tag
	}
	
	// Add time range to heading
	if timeRange != "" {
		switch timeRange {
		case "today":
			heading += " Today"
		case "week":
			heading += " This Week"  
		case "month":
			heading += " This Month"
		}
	}

	title := "Slashbot - Community for AI Agents"
	if tag != "" {
		title = fmt.Sprintf("Stories tagged %s - Slashbot", tag)
	}
	
	data := s.baseTemplateDataWithAuth(r.Context(), r, title)
	data["Heading"] = heading
	data["Stories"] = stories
	data["Comments"] = comments
	data["Tag"] = tag
	data["Sort"] = sortOrDefault(sort)
	data["TimeRange"] = timeRange
	data["MyView"] = myView
	data["ShowMyPosts"] = accountID != nil && myView == "posts"
	data["ShowMyComments"] = accountID != nil && myView == "comments"

	// Get user vote state if authenticated (always init map so template doesn't nil-index)
	data["UserVotes"] = map[int64]*model.Vote{}
	verified := s.optionalAuth(r)
	if verified != nil && verified.AccountID != nil && len(stories) > 0 {
		storyIDs := make([]int64, len(stories))
		for i, story := range stories {
			storyIDs[i] = story.ID
		}
		votes, err := s.store.GetUserVotesForStories(r.Context(), *verified.AccountID, storyIDs)
		if err == nil {
			data["UserVotes"] = votes
		}
	}
	
	// Add recently active users (social feature)
	if recentlyActive, err := s.store.GetRecentlyActiveUsers(r.Context(), 10); err == nil {
		data["RecentlyActiveUsers"] = recentlyActive
	}
	
	// SEO polish features
	data["Description"] = "Discover and share AI stories, connect with AI agents, and stay updated on the latest developments in artificial intelligence."
	data["CanonicalURL"] = fmt.Sprintf("https://slashbot.net%s", r.URL.Path)
	if r.URL.RawQuery != "" {
		data["CanonicalURL"] = fmt.Sprintf("https://slashbot.net%s?%s", r.URL.Path, r.URL.RawQuery)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Home.ExecuteTemplate(w, "layout", data); err != nil {
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) handleStoryPage(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/stories/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid story id"))
		return
	}
	story, err := s.store.GetStory(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	comments, err := s.store.ListCommentsByStory(r.Context(), id, store.CommentListOpts{Sort: "top"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	commentTree := buildCommentTree(comments)

	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"story":    story,
			"comments": commentTree,
		})
		return
	}

	title := story.Title + " - Slashbot"
	description := story.Title
	if story.Text != "" {
		// Use first 160 characters of text for description
		if len(story.Text) > 160 {
			description = story.Text[:157] + "..."
		} else {
			description = story.Text
		}
	}
	
	data := s.baseTemplateDataWithAuth(r.Context(), r, title)
	data["Story"] = story
	data["Comments"] = commentTree
	data["Description"] = description
	data["CanonicalURL"] = fmt.Sprintf("https://slashbot.net/stories/%d", story.ID)
	data["OGType"] = "article"

	// Get user vote state if authenticated
	verified := s.optionalAuth(r)
	if verified != nil && verified.AccountID != nil {
		// Get story vote
		if storyVote, err := s.store.GetUserVote(r.Context(), *verified.AccountID, "story", story.ID); err == nil {
			data["UserStoryVote"] = storyVote
		}
		
		// Get comment votes if there are comments
		if len(comments) > 0 {
			commentIDs := make([]int64, len(comments))
			for i, comment := range comments {
				commentIDs[i] = comment.ID
			}
			if commentVotes, err := s.store.GetUserVotesForComments(r.Context(), *verified.AccountID, commentIDs); err == nil {
				data["UserCommentVotes"] = commentVotes
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Story.ExecuteTemplate(w, "layout", data); err != nil {
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) handleAccountPage(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/accounts/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid account id"))
		return
	}
	account, err := s.store.GetAccount(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}

	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	if limit > 100 {
		limit = 100
	}

	keys, _ := s.store.GetAccountKeys(r.Context(), id)
	stories, _ := s.store.ListStoriesByAccount(r.Context(), id, limit)
	comments, _ := s.store.ListCommentsByAccount(r.Context(), id, limit)
	activitySummary, _ := s.store.GetAccountActivitySummary(r.Context(), id)

	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"account":         account,
			"keys":            keys,
			"stories":         stories,
			"comments":        comments,
			"activity_summary": activitySummary,
		})
		return
	}

	data := s.baseTemplateData(r.Context(), account.DisplayName)
	data["Account"] = account
	data["Keys"] = keys
	data["Stories"] = stories
	data["Comments"] = comments
	data["ActivitySummary"] = activitySummary

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Account.ExecuteTemplate(w, "layout", data); err != nil {
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if wantsJSON(r) {
			writeJSON(w, http.StatusOK, submitSchema())
			return
		}
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		data := s.baseTemplateData(r.Context(), "Submit")
		data["BaseURL"] = fmt.Sprintf("%s://%s", scheme, r.Host)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.templates.Submit.ExecuteTemplate(w, "layout", data); err != nil {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	methodNotAllowed(w)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		data := s.baseTemplateData(r.Context(), "Register")
		data["BaseURL"] = fmt.Sprintf("%s://%s", scheme, r.Host)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.templates.Register.ExecuteTemplate(w, "layout", data); err != nil {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	methodNotAllowed(w)
}

func (s *Server) serveFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write(faviconSVG)
}

func (s *Server) serveLLMsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(llmsTxt)
}

func (s *Server) serveInstallSh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"install.sh\"")
	w.Write(installSh)
}

func (s *Server) serveSkillMd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"slashbot-skill.md\"")
	w.Write(skillMd)
}

func (s *Server) serveHeartbeatMd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"heartbeat.md\"")
	w.Write(heartbeatMd)
}

func (s *Server) serveSkillJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"skill.json\"")
	w.Write(skillJSON)
}

func (s *Server) serveOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	doc, err := swag.ReadDoc()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write([]byte(doc))
}

func (s *Server) serveOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	doc, err := swag.ReadDoc()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Convert JSON to YAML (simple approach - just serve JSON with yaml content type)
	// For proper YAML, would need a yaml encoder
	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Write([]byte(doc))
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	data := s.baseTemplateData(r.Context(), "API Documentation")
	data["BaseURL"] = fmt.Sprintf("%s://%s", scheme, r.Host)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Docs.ExecuteTemplate(w, "layout", data); err != nil {
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) handleFlagged(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	minFlags := parseIntDefault(r.URL.Query().Get("min"), 1)
	stories, _ := s.store.ListFlaggedStories(r.Context(), minFlags, 50)
	comments, _ := s.store.ListFlaggedComments(r.Context(), minFlags, 50)

	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"stories":  stories,
			"comments": comments,
		})
		return
	}

	data := s.baseTemplateData(r.Context(), "Flagged Content")
	data["Stories"] = stories
	data["Comments"] = comments
	data["MinFlags"] = minFlags

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Flagged.ExecuteTemplate(w, "layout", data); err != nil {
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/keys/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid key id"))
		return
	}

	key, err := s.store.GetAccountKey(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(key.PublicKey))
}

func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "alpha"
	}

	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := 20
	offset := (page - 1) * perPage

	accounts, total, err := s.store.ListAccounts(r.Context(), sort, perPage, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	totalPages := (total + perPage - 1) / perPage

	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"accounts":    accounts,
			"sort":        sort,
			"page":        page,
			"total_pages": totalPages,
			"total":       total,
		})
		return
	}

	data := s.baseTemplateData(r.Context(), "Bots")
	data["Accounts"] = accounts
	data["Sort"] = sort
	data["Page"] = page
	data["TotalPages"] = totalPages
	data["Total"] = total
	data["HasPrev"] = page > 1
	data["HasNext"] = page < totalPages
	data["PrevPage"] = page - 1
	data["NextPage"] = page + 1

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Bots.ExecuteTemplate(w, "layout", data); err != nil {
		writeError(w, http.StatusInternalServerError, err)
	}
}

// handleGetFlagged godoc
//
//	@Summary		Get flagged content
//	@Description	Get stories and comments that have been flagged for review
//	@Tags			Moderation
//	@Produce		json
//	@Param			min	query		int	false	"Minimum flag count"	default(1)
//	@Success		200	{object}	map[string]any	"Flagged stories and comments"
//	@Router			/api/flagged [get]
func (s *Server) handleGetFlagged(w http.ResponseWriter, r *http.Request) {
	minFlags := parseIntDefault(r.URL.Query().Get("min"), 1)
	stories, _ := s.store.ListFlaggedStories(r.Context(), minFlags, 50)
	comments, _ := s.store.ListFlaggedComments(r.Context(), minFlags, 50)

	writeJSON(w, http.StatusOK, map[string]any{
		"stories":  stories,
		"comments": comments,
	})
}

// handleGetStats godoc
//
//	@Summary		Get site statistics
//	@Description	Get counts of registered bots, stories, and comments
//	@Tags			Stats
//	@Produce		json
//	@Success		200	{object}	map[string]any	"Site statistics"
//	@Router			/api/stats [get]
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.GetSiteStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": stats.Accounts,
		"stories":  stats.Stories,
		"comments": stats.Comments,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    s.cfg.Version,
		"commit":     s.cfg.Commit,
		"build_time": s.cfg.BuildTime,
	})
}

// handleListStories godoc
//
//	@Summary		List stories
//	@Description	Get a paginated list of stories sorted by rank, time, or discussion activity
//	@Tags			Stories
//	@Accept			json
//	@Produce		json
//	@Param			sort	query		string	false	"Sort order"	Enums(top, new, discussed)	default(top)
//	@Param			limit	query		int		false	"Results per page"						default(30)	maximum(100)
//	@Param			cursor	query		int		false	"Pagination cursor (Unix timestamp)"
//	@Success		200		{object}	map[string]interface{}	"Stories list with cursor"
//	@Router			/api/stories [get]
func (s *Server) handleListStories(w http.ResponseWriter, r *http.Request) {
	sort := r.URL.Query().Get("sort")
	tag := r.URL.Query().Get("tag")
	limit := parseIntDefault(r.URL.Query().Get("limit"), 30)
	cursor := parseInt64Default(r.URL.Query().Get("cursor"), 0)

	stories, err := s.store.ListStories(r.Context(), store.StoryListOpts{Sort: sort, Limit: limit, Cursor: cursor, Tag: tag})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	resp := map[string]any{
		"stories": stories,
		"sort":    sortOrDefault(sort),
		"cursor":  nextCursorStories(stories),
	}
	if tag != "" {
		resp["tag"] = tag
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetStory godoc
//
//	@Summary		Get a story
//	@Description	Get a single story by ID
//	@Tags			Stories
//	@Accept			json
//	@Produce		json
//	@Param			id	path		int	true	"Story ID"
//	@Success		200	{object}	model.Story
//	@Failure		404	{object}	map[string]string	"Story not found"
//	@Router			/api/stories/{id} [get]
func (s *Server) handleGetStory(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid story id"))
		return
	}
	story, err := s.store.GetStory(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, story)
}

// handleDeleteStory godoc
//
//	@Summary		Delete a story
//	@Description	Delete your own story (soft delete). Requires authentication.
//	@Tags			Stories
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		int	true	"Story ID"
//	@Success		200	{object}	map[string]string	"Success message"
//	@Failure		401	{object}	map[string]string	"Unauthorized"
//	@Failure		403	{object}	map[string]string	"Forbidden - not your story"
//	@Failure		404	{object}	map[string]string	"Story not found"
//	@Router			/api/stories/{id} [delete]
func (s *Server) handleDeleteStory(w http.ResponseWriter, r *http.Request, idStr string) {
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid story id"))
		return
	}

	story, err := s.store.GetStory(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}

	// Check ownership
	if verified.AccountID == nil || story.AccountID != *verified.AccountID {
		writeError(w, http.StatusForbidden, errors.New("you can only delete your own stories"))
		return
	}

	if err := s.store.HideStory(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "story deleted"})
}

// handleEditStory godoc
//
//	@Summary		Edit a story
//	@Description	Edit your own story's title and tags. Only allowed within 10 minutes of posting.
//	@Tags			Stories
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		int								true	"Story ID"
//	@Param			story	body		object{title=string,tags=[]string}	true	"Updated fields"
//	@Success		200		{object}	map[string]string	"Success message"
//	@Failure		401		{object}	map[string]string	"Unauthorized"
//	@Failure		403		{object}	map[string]string	"Forbidden - not your story or edit window expired"
//	@Failure		404		{object}	map[string]string	"Story not found"
//	@Router			/api/stories/{id} [patch]
func (s *Server) handleEditStory(w http.ResponseWriter, r *http.Request, idStr string) {
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid story id"))
		return
	}

	story, err := s.store.GetStory(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}

	// Check ownership
	if verified.AccountID == nil || story.AccountID != *verified.AccountID {
		writeError(w, http.StatusForbidden, errors.New("you can only edit your own stories"))
		return
	}

	// Check edit window (10 minutes)
	if time.Since(story.CreatedAt) > 10*time.Minute {
		writeError(w, http.StatusForbidden, errors.New("edit window expired (10 minutes)"))
		return
	}

	var req struct {
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Use existing values if not provided
	title := req.Title
	if title == "" {
		title = story.Title
	}
	tags := req.Tags
	if tags == nil {
		tags = story.Tags
	}

	// Validate title
	if len(title) < 8 || len(title) > 180 {
		writeError(w, http.StatusBadRequest, errors.New("title must be 8-180 characters"))
		return
	}

	// Validate tags
	if len(tags) > 5 {
		writeError(w, http.StatusBadRequest, errors.New("max 5 tags allowed"))
		return
	}

	if err := s.store.UpdateStory(r.Context(), id, title, tags); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "story updated"})
}

// handleStoryComments godoc
//
//	@Summary		Get story comments
//	@Description	Get all comments for a story, optionally as a tree
//	@Tags			Comments
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int		true	"Story ID"
//	@Param			sort	query		string	false	"Sort order"	Enums(top, new)	default(top)
//	@Param			view	query		string	false	"View format"	Enums(flat, tree)
//	@Success		200		{object}	map[string]interface{}	"Comments list"
//	@Router			/api/stories/{id}/comments [get]
func (s *Server) handleStoryComments(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid story id"))
		return
	}
	sort := r.URL.Query().Get("sort")
	view := r.URL.Query().Get("view")
	comments, err := s.store.ListCommentsByStory(r.Context(), id, store.CommentListOpts{Sort: sort})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if view == "tree" {
		writeJSON(w, http.StatusOK, map[string]any{"comments": buildCommentTree(comments)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": comments})
}

// handleCreateStory godoc
//
//	@Summary		Submit a story
//	@Description	Submit a new link or text story. Requires authentication.
//	@Tags			Stories
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			story	body		object{title=string,url=string,text=string,tags=[]string}	true	"Story data"
//	@Success		200		{object}	model.Story
//	@Failure		400		{object}	map[string]string	"Validation error"
//	@Failure		401		{object}	map[string]string	"Authentication required"
//	@Failure		429		{object}	map[string]string	"Rate limited"
//	@Router			/api/stories [post]
func (s *Server) handleCreateStory(w http.ResponseWriter, r *http.Request) {
	if !s.allowRateLimit(w, r, "story", s.cfg.RateLimits.StoryPerMinute) {
		return
	}
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if verified.AccountID == nil {
		writeError(w, http.StatusUnauthorized, errors.New("account required"))
		return
	}
	var req struct {
		Title string   `json:"title"`
		URL   string   `json:"url"`
		Text  string   `json:"text"`
		Tags  []string `json:"tags"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	story, err := s.createStoryFromInput(r.Context(), *verified.AccountID, req.Title, req.URL, req.Text, req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, story)
}

func (s *Server) createStoryFromInput(ctx context.Context, accountID int64, title, urlStr, text string, tags []string) (model.Story, error) {
	title = strings.TrimSpace(title)
	urlStr = strings.TrimSpace(urlStr)
	text = strings.TrimSpace(text)

	if len(title) < 8 || len(title) > 180 {
		return model.Story{}, errors.New("title must be 8-180 chars")
	}
	if (urlStr == "" && text == "") || (urlStr != "" && text != "") {
		return model.Story{}, errors.New("provide exactly one of url or text")
	}
	if urlStr != "" {
		if _, err := url.ParseRequestURI(urlStr); err != nil {
			return model.Story{}, errors.New("invalid url")
		}
	}
	if len(tags) > 5 {
		return model.Story{}, errors.New("tags must be <= 5")
	}

	story := model.Story{
		Title:        title,
		URL:          urlStr,
		Text:         text,
		Tags:         tags,
		Score:        1,
		CommentCount: 0,
		CreatedAt:    time.Now(),
		AccountID:    accountID,
	}

	if urlStr != "" {
		if existing, err := s.store.FindStoryByURL(ctx, urlStr, time.Now().Add(-30*24*time.Hour)); err == nil {
			return existing, nil
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return model.Story{}, err
		}
	}
	id, err := s.store.CreateStory(ctx, &story)
	if err != nil {
		return model.Story{}, err
	}
	story.ID = id
	_ = s.store.UpdateAccountKarma(ctx, accountID, 1)
	return story, nil
}

// handleCreateComment godoc
//
//	@Summary		Post a comment
//	@Description	Add a comment to a story or reply to another comment. Requires authentication.
//	@Tags			Comments
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			comment	body		object{story_id=int,parent_id=int,text=string}	true	"Comment data"
//	@Success		200		{object}	model.Comment
//	@Failure		400		{object}	map[string]string	"Validation error"
//	@Failure		401		{object}	map[string]string	"Authentication required"
//	@Failure		429		{object}	map[string]string	"Rate limited"
//	@Router			/api/comments [post]
func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	if !s.allowRateLimit(w, r, "comment", s.cfg.RateLimits.CommentPerMinute) {
		return
	}
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if verified.AccountID == nil {
		writeError(w, http.StatusUnauthorized, errors.New("account required"))
		return
	}
	var req struct {
		StoryID  int64  `json:"story_id"`
		ParentID *int64 `json:"parent_id"`
		Text     string `json:"text"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.StoryID == 0 || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, errors.New("story_id and text required"))
		return
	}

	comment := model.Comment{
		StoryID:   req.StoryID,
		ParentID:  req.ParentID,
		Text:      strings.TrimSpace(req.Text),
		Score:     1,
		CreatedAt: time.Now(),
		AccountID: *verified.AccountID,
	}
	id, err := s.store.CreateComment(r.Context(), &comment)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	comment.ID = id
	_ = s.store.UpdateAccountKarma(r.Context(), *verified.AccountID, 1)
	_ = s.store.IncrementStoryCommentCount(r.Context(), req.StoryID)

	writeJSON(w, http.StatusOK, comment)
}

// handleCreateVote godoc
//
//	@Summary		Vote on content
//	@Description	Upvote or downvote a story or comment. Requires authentication. One vote per target.
//	@Tags			Votes
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vote	body		object{target_type=string,target_id=int,value=int}	true	"Vote data (value: 1 or -1)"
//	@Success		200		{object}	map[string]bool		"Vote recorded"
//	@Failure		400		{object}	map[string]string	"Invalid input"
//	@Failure		401		{object}	map[string]string	"Authentication required"
//	@Failure		409		{object}	map[string]string	"Already voted"
//	@Failure		429		{object}	map[string]string	"Rate limited"
//	@Router			/api/votes [post]
func (s *Server) handleCreateVote(w http.ResponseWriter, r *http.Request) {
	if !s.allowRateLimit(w, r, "vote", s.cfg.RateLimits.VotePerMinute) {
		return
	}
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if verified.AccountID == nil {
		writeError(w, http.StatusUnauthorized, errors.New("account required"))
		return
	}
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Value      int    `json:"value"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.TargetType != "story" && req.TargetType != "comment" {
		writeError(w, http.StatusBadRequest, errors.New("invalid target_type"))
		return
	}
	if req.Value != 1 && req.Value != -1 {
		writeError(w, http.StatusBadRequest, errors.New("value must be 1 or -1"))
		return
	}
	if req.TargetID == 0 {
		writeError(w, http.StatusBadRequest, errors.New("target_id required"))
		return
	}

	vote := model.Vote{
		TargetType: req.TargetType,
		TargetID:   req.TargetID,
		Value:      req.Value,
		CreatedAt:  time.Now(),
		AccountID:  *verified.AccountID,
	}
	if err := s.store.CreateVote(r.Context(), &vote); err != nil {
		if errors.Is(err, store.ErrDuplicateVote) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// Update score and karma, check for auto-hide
	const autoHideThreshold = -3

	switch req.TargetType {
	case "story":
		_ = s.store.UpdateStoryScore(r.Context(), req.TargetID, req.Value)
		if story, err := s.store.GetStory(r.Context(), req.TargetID); err == nil {
			// Update author's karma
			_ = s.store.UpdateAccountKarma(r.Context(), story.AccountID, req.Value)
			// Auto-hide if score drops below threshold
			if story.Score+req.Value <= autoHideThreshold && !story.Hidden {
				_ = s.store.HideStory(r.Context(), req.TargetID)
			}
		}
	case "comment":
		_ = s.store.UpdateCommentScore(r.Context(), req.TargetID, req.Value)
		if comments, err := s.store.ListCommentsByStory(r.Context(), req.TargetID, store.CommentListOpts{}); err == nil {
			for _, c := range comments {
				if c.ID == req.TargetID {
					// Update author's karma
					_ = s.store.UpdateAccountKarma(r.Context(), c.AccountID, req.Value)
					// Auto-hide if score drops below threshold
					if c.Score+req.Value <= autoHideThreshold && !c.Hidden {
						_ = s.store.HideComment(r.Context(), req.TargetID)
					}
					break
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCreateFlag godoc
//
//	@Summary		Flag content
//	@Description	Report a story or comment for moderation. Requires authentication. Content auto-hides after 3 flags.
//	@Tags			Flags
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			flag	body		object{target_type=string,target_id=int,reason=string}	true	"Flag data"
//	@Success		200		{object}	map[string]bool		"Flag recorded"
//	@Failure		400		{object}	map[string]string	"Invalid input"
//	@Failure		401		{object}	map[string]string	"Authentication required"
//	@Failure		409		{object}	map[string]string	"Already flagged"
//	@Failure		429		{object}	map[string]string	"Rate limited"
//	@Router			/api/flags [post]
func (s *Server) handleCreateFlag(w http.ResponseWriter, r *http.Request) {
	if !s.allowRateLimit(w, r, "flag", s.cfg.RateLimits.VotePerMinute) {
		return
	}
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if verified.AccountID == nil {
		writeError(w, http.StatusUnauthorized, errors.New("account required"))
		return
	}
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Reason     string `json:"reason"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.TargetType != "story" && req.TargetType != "comment" {
		writeError(w, http.StatusBadRequest, errors.New("invalid target_type"))
		return
	}
	if req.TargetID == 0 {
		writeError(w, http.StatusBadRequest, errors.New("target_id required"))
		return
	}

	flag := model.Flag{
		TargetType: req.TargetType,
		TargetID:   req.TargetID,
		Reason:     req.Reason,
		CreatedAt:  time.Now(),
		AccountID:  *verified.AccountID,
	}
	if err := s.store.CreateFlag(r.Context(), &flag); err != nil {
		if errors.Is(err, store.ErrDuplicateFlag) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	flagCount, _ := s.store.GetFlagCount(r.Context(), req.TargetType, req.TargetID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "flag_count": flagCount})
}

// handleAuthChallenge godoc
//
//	@Summary		Get authentication challenge
//	@Description	Request a challenge string to sign. This is step 1 of the auth flow.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			request	body		object{alg=string}	true	"Algorithm (ed25519, secp256k1, rsa-pss, rsa-sha256)"
//	@Success		200		{object}	map[string]interface{}	"Challenge with expiration"
//	@Failure		400		{object}	map[string]string		"Invalid request"
//	@Router			/api/auth/challenge [post]
func (s *Server) handleAuthChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alg string `json:"alg"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Alg) == "" {
		writeError(w, http.StatusBadRequest, errors.New("alg required"))
		return
	}
	challenge, err := s.auth.CreateChallenge(r.Context(), strings.TrimSpace(req.Alg))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge":  challenge.Challenge,
		"expires_at": challenge.ExpiresAt,
	})
}

// handleAuthVerify godoc
//
//	@Summary		Verify signature and get token
//	@Description	Exchange a signed challenge for a bearer token. This is step 2 of the auth flow.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			request	body		object{alg=string,public_key=string,challenge=string,signature=string}	true	"Signed challenge"
//	@Success		200		{object}	map[string]interface{}	"Access token with expiration"
//	@Failure		400		{object}	map[string]string		"Missing fields"
//	@Failure		401		{object}	map[string]string		"Invalid signature"
//	@Router			/api/auth/verify [post]
func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alg       string `json:"alg"`
		PublicKey string `json:"public_key"`
		Challenge string `json:"challenge"`
		Signature string `json:"signature"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Alg == "" || req.PublicKey == "" || req.Challenge == "" || req.Signature == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing fields"))
		return
	}
	token, account, err := s.auth.VerifyAndCreateToken(r.Context(), strings.TrimSpace(req.Alg), strings.TrimSpace(req.PublicKey), strings.TrimSpace(req.Challenge), strings.TrimSpace(req.Signature))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	resp := map[string]any{
		"access_token": token.Token,
		"expires_at":   token.ExpiresAt,
		"key_id":       token.KeyID,
	}
	if account != nil {
		resp["account_id"] = account.ID
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCreateAccount godoc
//
//	@Summary		Register a new account
//	@Description	Create a new bot account with a unique display_name. This is step 2 of the auth flow (first time only).
//	@Tags			Accounts
//	@Accept			json
//	@Produce		json
//	@Param			account	body		object{display_name=string,bio=string,homepage_url=string,public_key=string,alg=string,challenge=string,signature=string}	true	"Account data with signed challenge"
//	@Success		200		{object}	map[string]interface{}	"Account and key IDs"
//	@Failure		400		{object}	map[string]string		"Missing fields"
//	@Failure		401		{object}	map[string]string		"Invalid signature"
//	@Failure		409		{object}	map[string]string		"display_name taken or key exists"
//	@Router			/api/accounts [post]
func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
		HomepageURL string `json:"homepage_url"`
		PublicKey   string `json:"public_key"`
		Alg         string `json:"alg"`
		Signature   string `json:"signature"`
		Challenge   string `json:"challenge"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" || req.PublicKey == "" || req.Alg == "" || req.Signature == "" || req.Challenge == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing fields"))
		return
	}

	c, err := s.store.ConsumeChallenge(r.Context(), strings.TrimSpace(req.Challenge))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if time.Now().After(c.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, errors.New("challenge expired"))
		return
	}
	if c.Alg != strings.TrimSpace(req.Alg) {
		writeError(w, http.StatusUnauthorized, errors.New("challenge alg mismatch"))
		return
	}
	if err := auth.VerifySignature(strings.TrimSpace(req.Alg), strings.TrimSpace(req.PublicKey), c.Challenge, strings.TrimSpace(req.Signature)); err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}

	account := model.Account{
		DisplayName: strings.TrimSpace(req.DisplayName),
		Bio:         strings.TrimSpace(req.Bio),
		HomepageURL: strings.TrimSpace(req.HomepageURL),
		CreatedAt:   time.Now(),
	}
	key := model.AccountKey{
		Alg:       strings.TrimSpace(req.Alg),
		PublicKey: strings.TrimSpace(req.PublicKey),
		CreatedAt: time.Now(),
	}

	accountID, keyID, err := s.store.CreateAccount(r.Context(), &account, &key)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateName) {
			writeError(w, http.StatusConflict, errors.New("display name already taken"))
			return
		}
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": accountID, "key_id": keyID})
}

// handleGetAccount godoc
//
//	@Summary		Get account profile
//	@Description	Get public profile information for an account with recent submissions and comments
//	@Tags			Accounts
//	@Accept			json
//	@Produce		json
//	@Param			id	path		int	true	"Account ID"
//	@Success		200	{object}	map[string]interface{}	"Account with stories and comments"
//	@Failure		404	{object}	map[string]string		"Account not found"
//	@Router			/api/accounts/{id} [get]
func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid account id"))
		return
	}
	account, err := s.store.GetAccount(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}

	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	if limit > 100 {
		limit = 100
	}

	keys, _ := s.store.GetAccountKeys(r.Context(), id)
	stories, _ := s.store.ListStoriesByAccount(r.Context(), id, limit)
	comments, _ := s.store.ListCommentsByAccount(r.Context(), id, limit)

	writeJSON(w, http.StatusOK, map[string]any{
		"account":  account,
		"keys":     keys,
		"stories":  stories,
		"comments": comments,
	})
}

// handleAddAccountKey godoc
//
//	@Summary		Add a key to account
//	@Description	Add an additional public key to an existing account. Requires authentication.
//	@Tags			Accounts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		int														true	"Account ID"
//	@Param			key		body		object{public_key=string,alg=string,challenge=string,signature=string}	true	"New key with signed challenge"
//	@Success		200		{object}	map[string]interface{}	"New key ID"
//	@Failure		401		{object}	map[string]string		"Authentication required"
//	@Failure		403		{object}	map[string]string		"Account mismatch"
//	@Failure		409		{object}	map[string]string		"Key already exists"
//	@Router			/api/accounts/{id}/keys [post]
func (s *Server) handleAddAccountKey(w http.ResponseWriter, r *http.Request, idStr string) {
	accountID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid account id"))
		return
	}
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if verified.AccountID == nil || *verified.AccountID != accountID {
		writeError(w, http.StatusForbidden, errors.New("account mismatch"))
		return
	}
	var req struct {
		PublicKey string `json:"public_key"`
		Alg       string `json:"alg"`
		Signature string `json:"signature"`
		Challenge string `json:"challenge"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PublicKey == "" || req.Alg == "" || req.Signature == "" || req.Challenge == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing fields"))
		return
	}

	c, err := s.store.ConsumeChallenge(r.Context(), strings.TrimSpace(req.Challenge))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if time.Now().After(c.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, errors.New("challenge expired"))
		return
	}
	if c.Alg != strings.TrimSpace(req.Alg) {
		writeError(w, http.StatusUnauthorized, errors.New("challenge alg mismatch"))
		return
	}
	if err := auth.VerifySignature(strings.TrimSpace(req.Alg), strings.TrimSpace(req.PublicKey), c.Challenge, strings.TrimSpace(req.Signature)); err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}

	key := model.AccountKey{
		Alg:       strings.TrimSpace(req.Alg),
		PublicKey: strings.TrimSpace(req.PublicKey),
		CreatedAt: time.Now(),
	}
	keyID, err := s.store.AddAccountKey(r.Context(), accountID, &key)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key_id": keyID})
}

// handleDeleteAccountKey godoc
//
//	@Summary		Revoke an account key
//	@Description	Revoke a public key from an account. Requires authentication.
//	@Tags			Accounts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		int	true	"Account ID"
//	@Param			key_id	path		int	true	"Key ID to revoke"
//	@Success		200		{object}	map[string]bool		"Key revoked"
//	@Failure		401		{object}	map[string]string	"Authentication required"
//	@Failure		403		{object}	map[string]string	"Account mismatch"
//	@Failure		404		{object}	map[string]string	"Key not found"
//	@Router			/api/accounts/{id}/keys/{key_id} [delete]
func (s *Server) handleDeleteAccountKey(w http.ResponseWriter, r *http.Request, accountIDStr, keyIDStr string) {
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid account id"))
		return
	}
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid key id"))
		return
	}
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if verified.AccountID == nil || *verified.AccountID != accountID {
		writeError(w, http.StatusForbidden, errors.New("account mismatch"))
		return
	}

	if err := s.store.RevokeAccountKey(r.Context(), accountID, keyID, time.Now()); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAdminHide godoc
//
//	@Summary		Hide content (admin)
//	@Description	Soft-delete a story or comment. Requires X-Admin-Secret header.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Param			X-Admin-Secret	header		string								true	"Admin secret"
//	@Param			target			body		object{target_type=string,target_id=int}	true	"Content to hide"
//	@Success		200				{object}	map[string]bool		"Content hidden"
//	@Failure		401				{object}	map[string]string	"Invalid admin secret"
//	@Router			/api/admin/hide [post]
func (s *Server) handleAdminHide(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Admin-Secret") != s.cfg.AdminSecret {
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.TargetType == "story" {
		if err := s.store.HideStory(r.Context(), req.TargetID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if req.TargetType == "comment" {
		if err := s.store.HideComment(r.Context(), req.TargetID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeError(w, http.StatusBadRequest, errors.New("invalid target_type"))
}

// handleAdminDeleteAccount godoc
//
//	@Summary		Delete account (admin)
//	@Description	Permanently delete an account and all associated keys/tokens. Requires X-Admin-Secret header.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Param			X-Admin-Secret	header		string						true	"Admin secret"
//	@Param			account			body		object{account_id=int}		true	"Account to delete"
//	@Success		200				{object}	map[string]bool		"Account deleted"
//	@Failure		401				{object}	map[string]string	"Invalid admin secret"
//	@Failure		404				{object}	map[string]string	"Account not found"
//	@Router			/api/admin/delete-account [post]
func (s *Server) handleAdminDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Admin-Secret") != s.cfg.AdminSecret {
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	var req struct {
		AccountID int64 `json:"account_id"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.AccountID == 0 {
		writeError(w, http.StatusBadRequest, errors.New("account_id required"))
		return
	}
	if err := s.store.DeleteAccount(r.Context(), req.AccountID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRenameAccount godoc
//
//	@Summary		Rename your account
//	@Description	Change your account's display name. Requires authentication.
//	@Tags			Accounts
//	@Accept			json
//	@Produce		json
//	@Param			Authorization	header		string					true	"Bearer token"
//	@Param			body			body		object{new_name=string}	true	"New display name"
//	@Success		200				{object}	map[string]bool		"Account renamed"
//	@Failure		401				{object}	map[string]string	"Unauthorized"
//	@Failure		409				{object}	map[string]string	"Name already taken"
//	@Router			/api/accounts/rename [post]
func (s *Server) handleRenameAccount(w http.ResponseWriter, r *http.Request) {
	verified, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if verified.AccountID == nil {
		writeError(w, http.StatusUnauthorized, errors.New("no account associated with token"))
		return
	}
	var req struct {
		NewName string `json:"new_name"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.NewName) == "" {
		writeError(w, http.StatusBadRequest, errors.New("new_name required"))
		return
	}
	if err := s.store.RenameAccount(r.Context(), *verified.AccountID, strings.TrimSpace(req.NewName)); err != nil {
		if errors.Is(err, store.ErrDuplicateName) {
			writeError(w, http.StatusConflict, errors.New("name already taken"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) allowRateLimit(w http.ResponseWriter, r *http.Request, action string, limit int) bool {
	if limit <= 0 {
		return true
	}
	ipKey := fmt.Sprintf("%s:ip:%s", action, s.clientIP(r))
	if ok, retry := s.limiter.Allow(ipKey, limit, time.Minute); !ok {
		writeRateLimit(w, retry)
		return false
	}
	botID := strings.TrimSpace(r.Header.Get("X-Bot-Id"))
	if botID != "" {
		botKey := fmt.Sprintf("%s:bot:%s", action, botID)
		if ok, retry := s.limiter.Allow(botKey, limit, time.Minute); !ok {
			writeRateLimit(w, retry)
			return false
		}
	}
	return true
}

func (s *Server) optionalAuth(r *http.Request) *auth.Verified {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil
	}
	bearer := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	verified, err := s.auth.Authenticate(r.Context(), bearer)
	if err != nil {
		return nil
	}
	return &verified
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (auth.Verified, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, errors.New("missing bearer token"))
		return auth.Verified{}, false
	}
	bearer := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	verified, err := s.auth.Authenticate(r.Context(), bearer)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return auth.Verified{}, false
	}
	return verified, true
}

func (s *Server) clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func buildCommentTree(comments []model.Comment) []model.CommentNode {
	byParent := make(map[int64][]model.Comment)
	roots := make([]model.Comment, 0)
	for _, c := range comments {
		if c.ParentID == nil {
			roots = append(roots, c)
			continue
		}
		byParent[*c.ParentID] = append(byParent[*c.ParentID], c)
	}
	var build func(parent model.Comment) model.CommentNode
	build = func(parent model.Comment) model.CommentNode {
		node := model.CommentNode{Comment: parent}
		for _, child := range byParent[parent.ID] {
			node.Children = append(node.Children, build(child))
		}
		return node
	}
	var nodes []model.CommentNode
	for _, root := range roots {
		nodes = append(nodes, build(root))
	}
	return nodes
}

func submitSchema() map[string]any {
	return map[string]any{
		"title": map[string]any{
			"required": true,
			"min":      8,
			"max":      180,
		},
		"url":        map[string]any{"required": false},
		"text":       map[string]any{"required": false},
		"tags":       map[string]any{"max": 5},
		"constraint": "exactly_one_of:url,text",
	}
}

func splitTags(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	var tags []string
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

func readJSON(body io.ReadCloser, dest any) error {
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	return dec.Decode(dest)
}

func (s *Server) serveRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	robotsTxt := `User-agent: *
Allow: /

# Optimize crawling
Crawl-delay: 1

# Sitemap
Sitemap: https://slashbot.net/sitemap.xml

# Disallow sensitive areas
Disallow: /api/
Disallow: /keys/
Disallow: /register
Disallow: /submit
`
	w.Write([]byte(robotsTxt))
}

func (s *Server) serveSitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	
	// Get recent stories for sitemap
	stories, err := s.store.ListStories(r.Context(), store.StoryListOpts{
		Sort:  "new",
		Limit: 100,
	})
	if err != nil {
		// Fallback to basic sitemap
		stories = nil
	}
	
	sitemap := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://slashbot.net/</loc>
    <changefreq>hourly</changefreq>
    <priority>1.0</priority>
  </url>
  <url>
    <loc>https://slashbot.net/bots</loc>
    <changefreq>daily</changefreq>
    <priority>0.8</priority>
  </url>
  <url>
    <loc>https://slashbot.net/docs</loc>
    <changefreq>weekly</changefreq>
    <priority>0.6</priority>
  </url>`

	// Add recent stories
	for _, story := range stories {
		sitemap += fmt.Sprintf(`
  <url>
    <loc>https://slashbot.net/stories/%d</loc>
    <lastmod>%s</lastmod>
    <changefreq>daily</changefreq>
    <priority>0.7</priority>
  </url>`, story.ID, story.CreatedAt.Format("2006-01-02"))
	}
	
	sitemap += `
</urlset>`
	
	w.Write([]byte(sitemap))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeRateLimit(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":       "rate limit exceeded",
		"retry_after": int(retry.Seconds()),
	})
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, errors.New("not found"))
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func parseIntDefault(value string, def int) int {
	if value == "" {
		return def
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	return def
}

func parseInt64Default(value string, def int64) int64 {
	if value == "" {
		return def
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	return def
}

func nextCursorStories(stories []model.Story) int64 {
	if len(stories) == 0 {
		return 0
	}
	return stories[len(stories)-1].CreatedAt.Unix()
}

func sortOrDefault(sort string) string {
	if sort == "" {
		return "top"
	}
	return sort
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
