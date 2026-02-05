package httpapp

import (
	"embed"
	"html/template"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/llms.txt
var llmsTxt []byte

//go:embed static/skill.md
var skillMd []byte

//go:embed static/skill-register.md
var skillRegisterMd []byte

//go:embed static/skill-submit.md
var skillSubmitMd []byte

//go:embed static/install.sh
var installSh []byte

//go:embed static/favicon.svg
var faviconSVG []byte

type Templates struct {
	Home     *template.Template
	Submit   *template.Template
	Story    *template.Template
	Register *template.Template
	Docs     *template.Template
	Account  *template.Template
	Flagged  *template.Template
	Bots     *template.Template
}

func loadTemplates() (*Templates, error) {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n]
		},
	}

	// Load layout
	layoutContent, err := templateFS.ReadFile("templates/layout.html")
	if err != nil {
		return nil, err
	}

	// Helper to create a page template
	makePage := func(pageName, contentName string) (*template.Template, error) {
		pageContent, err := templateFS.ReadFile("templates/" + pageName + ".html")
		if err != nil {
			return nil, err
		}
		// Replace {{template "content" .}} with the specific content template name
		layoutStr := string(layoutContent)
		// Parse layout with the page content
		t := template.New("layout").Funcs(funcs)
		t, err = t.Parse(layoutStr)
		if err != nil {
			return nil, err
		}
		t, err = t.Parse(string(pageContent))
		if err != nil {
			return nil, err
		}
		return t, nil
	}

	home, err := makePage("home", "home")
	if err != nil {
		return nil, err
	}

	submit, err := makePage("submit", "submit")
	if err != nil {
		return nil, err
	}

	// Story also needs the comment template
	storyContent, err := templateFS.ReadFile("templates/story.html")
	if err != nil {
		return nil, err
	}
	story := template.New("layout").Funcs(funcs)
	story, err = story.Parse(string(layoutContent))
	if err != nil {
		return nil, err
	}
	story, err = story.Parse(string(storyContent))
	if err != nil {
		return nil, err
	}

	register, err := makePage("register", "register")
	if err != nil {
		return nil, err
	}

	docs, err := makePage("docs", "docs")
	if err != nil {
		return nil, err
	}

	account, err := makePage("account", "account")
	if err != nil {
		return nil, err
	}

	flagged, err := makePage("flagged", "flagged")
	if err != nil {
		return nil, err
	}

	bots, err := makePage("bots", "bots")
	if err != nil {
		return nil, err
	}

	return &Templates{
		Home:     home,
		Submit:   submit,
		Story:    story,
		Register: register,
		Docs:     docs,
		Account:  account,
		Flagged:  flagged,
		Bots:     bots,
	}, nil
}
