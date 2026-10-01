package blog

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"
)

func TestDocumentedBlogTemplatesRenderPageTypes(t *testing.T) {
	for _, file := range []string{"../webui/assets/plugin-guide.zh.md", "../webui/assets/plugin-guide.md", "../../docs/server-plugins.md"} {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sections := strings.Split(string(raw), "```gotemplate\n")
			snippet, _, found := strings.Cut(sections[len(sections)-1], "```")
			if len(sections) < 2 || !found {
				t.Fatal("template example missing")
			}
			tpl, err := template.New("guide").Funcs(customThemeFuncs()).Option("missingkey=zero").Parse(snippet)
			if err != nil {
				t.Fatal(err)
			}
			for _, page := range []struct {
				params renderParams
				want   string
			}{
				{renderParams{IsHome: true, Title: "Blog", HomePosts: []HomePost{{Title: "<Post>", URL: "/p/example", Summary: "Summary"}}}, `href="/p/example">&lt;Post&gt;</a>`},
				{renderParams{IsFolder: true, FolderTitle: "Folder", ContentHTML: template.HTML(`<ul><li>Entry</li></ul>`)}, `<nav><ul><li>Entry</li></ul></nav>`},
				{renderParams{ArticleTitle: "Article", ContentHTML: template.HTML(`<p>Body</p>`)}, `<article><p>Body</p></article>`},
			} {
				var out bytes.Buffer
				if err := tpl.Execute(&out, page.params); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), page.want) {
					t.Fatalf("missing %q in %s", page.want, out.String())
				}
			}
		})
	}
}
