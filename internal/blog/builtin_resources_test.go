package blog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuiltinThemeCopies 保证发布二进制包含模板创建及复制所需的完整资源
func TestBuiltinThemeCopies(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateDevelopmentTheme(dir, "development-copy"); err != nil {
		t.Fatal(err)
	}
	for _, name := range BuiltinThemeNames {
		t.Run(name, func(t *testing.T) {
			target, err := ScaffoldTheme(dir, name, name+"-copy")
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range []string{"template.html", "style.css", "theme.js", "README.md"} {
				content, err := os.ReadFile(filepath.Join(target, file))
				if err != nil || len(content) == 0 {
					t.Fatalf("%s: empty or absent: %v", file, err)
				}
			}
		})
	}
}
