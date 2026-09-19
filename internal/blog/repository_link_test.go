package blog

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"
	"testing"
)

const repositoryURL = "https://github.com/helantianshen/oss-sync"

func TestProjectTemplatesIncludeRepositoryIdentity(t *testing.T) {
	t.Parallel()

	files := []struct {
		name string
		read func() ([]byte, error)
	}{
		{name: "default", read: func() ([]byte, error) { return templatesFS.ReadFile("templates/base.html") }},
		{name: "papertrail", read: func() ([]byte, error) { return themeAssetsFS.ReadFile("assets/papertrail/template.html") }},
		{name: "public home", read: func() ([]byte, error) { return templatesFS.ReadFile("templates/public_home.html") }},
		{name: "removed", read: func() ([]byte, error) { return templatesFS.ReadFile("templates/removed.html") }},
	}
	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			raw, err := file.read()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), repositoryURL) {
				t.Fatalf("template does not identify repository %s", repositoryURL)
			}
			if !strings.Contains(string(raw), `Powered by <a href="`+repositoryURL+`"`) || !strings.Contains(string(raw), `>OSS Sync</a>`) {
				t.Fatal("template does not show the linked OSS Sync attribution")
			}
		})
	}
}

func TestProjectThemeStylesCenterRepositoryIdentity(t *testing.T) {
	t.Parallel()

	styles := []struct {
		name string
		read func() ([]byte, error)
	}{
		{name: "default", read: func() ([]byte, error) { return themeAssetsFS.ReadFile("assets/default/style.css") }},
		{name: "papertrail", read: func() ([]byte, error) { return themeAssetsFS.ReadFile("assets/papertrail/style.css") }},
		{name: "development", read: func() ([]byte, error) { return themeAssetsFS.ReadFile("assets/development-template/style.css") }},
	}
	for _, style := range styles {
		t.Run(style.name, func(t *testing.T) {
			raw, err := style.read()
			if err != nil {
				t.Fatal(err)
			}
			css := string(raw)
			if !strings.Contains(css, ".oss-repository-link") || !strings.Contains(css, "width: 100%") || !strings.Contains(css, "text-align: center") {
				t.Fatal("repository identity is not centered across the full footer row")
			}
		})
	}
}

func TestBuiltInArticleTemplatesRenderVisibleArticleTitle(t *testing.T) {
	t.Parallel()

	params := renderParams{ArticleTitle: "sport使用手册", ThemeBaseURL: "/themes/default"}
	tests := []struct {
		name   string
		render func() (string, error)
	}{
		{
			name: "default",
			render: func() (string, error) {
				tpl, err := template.ParseFS(templatesFS, "templates/base.html")
				if err != nil {
					return "", err
				}
				var out bytes.Buffer
				err = tpl.ExecuteTemplate(&out, "base.html", params)
				return out.String(), err
			},
		},
		{
			name: "papertrail",
			render: func() (string, error) {
				raw, err := themeAssetsFS.ReadFile("assets/papertrail/template.html")
				if err != nil {
					return "", err
				}
				tpl, err := template.New("papertrail").Parse(string(raw))
				if err != nil {
					return "", err
				}
				var out bytes.Buffer
				err = tpl.Execute(&out, params)
				return out.String(), err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, err := test.render()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(page, `data-article-title>sport使用手册</h1>`) {
				t.Fatalf("article title is not visible in rendered page: %s", page)
			}
		})
	}
}

func TestBuiltInThemeMobileTOC_keepsOpenButtonVisible(t *testing.T) {
	t.Parallel()

	navHidden := regexp.MustCompile(`(?s)\.reading-toc\s*\{[^}]*transform:\s*translateX\(-100%\)`)
	panelHidden := regexp.MustCompile(`(?s)\.reading-toc__panel\s*\{[^}]*transform:\s*translateX\(-100%\)`)
	panelZIndex := regexp.MustCompile(`(?s)\.reading-toc__panel\s*\{[^}]*z-index:\s*(\d+)`)
	backdropZIndex := regexp.MustCompile(`(?s)\.reading-toc__backdrop\s*\{[^}]*z-index:\s*(\d+)`)
	for _, theme := range []string{"default", "papertrail"} {
		t.Run(theme, func(t *testing.T) {
			raw, err := themeAssetsFS.ReadFile("assets/" + theme + "/style.css")
			if err != nil {
				t.Fatal(err)
			}
			mobileCSS := strings.SplitN(string(raw), "@media (max-width: 780px)", 2)
			if len(mobileCSS) != 2 {
				t.Fatal("mobile TOC media query is missing")
			}
			if navHidden.MatchString(mobileCSS[1]) {
				t.Fatal("mobile TOC hides the open button with the drawer panel")
			}
			if !panelHidden.MatchString(mobileCSS[1]) {
				t.Fatal("mobile TOC does not hide the drawer panel separately")
			}
			panelMatch := panelZIndex.FindStringSubmatch(mobileCSS[1])
			backdropMatch := backdropZIndex.FindStringSubmatch(mobileCSS[1])
			if len(panelMatch) != 2 || len(backdropMatch) != 2 || panelMatch[1] <= backdropMatch[1] {
				t.Fatal("mobile TOC drawer panel is not above its backdrop")
			}
		})
	}
}
