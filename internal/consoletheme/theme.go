// 控制台主题
package consoletheme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	BuiltinDefault = "default"
	maxTreeBytes   = 64 << 20
	maxEntries     = 512
)

var (
	ErrExists   = errors.New("服务器主题已存在")
	ErrReadOnly = errors.New("内置服务器主题只读")
	ErrNotFound = errors.New("服务器主题不存在")
	themeNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

type Info struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	FileCount int    `json:"file_count"`
	Size      int64  `json:"size"`
}

func ValidateName(name string) error {
	if !themeNameRE.MatchString(name) {
		return errors.New("服务器主题名称只能使用字母、数字、连字符和下划线，且长度为 1–64")
	}
	return nil
}

func IsBuiltin(name string) bool {
	return name == BuiltinDefault
}

func Exists(dataDir, name string) bool {
	if IsBuiltin(name) {
		return true
	}
	dir, err := themeDir(dataDir, name)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "theme.css"))
	return err == nil && info.Mode().IsRegular()
}

func List(dataDir string) ([]Info, error) {
	count, size := builtinStats()
	themes := []Info{{Name: BuiltinDefault, Source: "builtin", FileCount: count, Size: size}}
	root := filepath.Join(dataDir, "console-themes")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return themes, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取服务器主题目录: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || ValidateName(entry.Name()) != nil || !Exists(dataDir, entry.Name()) {
			continue
		}
		fileCount, totalSize, statErr := dirStats(filepath.Join(root, entry.Name()))
		if statErr != nil {
			continue
		}
		themes = append(themes, Info{
			Name: entry.Name(), Source: "custom", FileCount: fileCount, Size: totalSize,
		})
	}
	sort.Slice(themes[1:], func(i, j int) bool {
		return themes[i+1].Name < themes[j+1].Name
	})
	return themes, nil
}

func Scaffold(dataDir, base, newName string) (string, error) {
	if err := ValidateName(newName); err != nil {
		return "", err
	}
	if IsBuiltin(newName) {
		return "", ErrReadOnly
	}
	if !Exists(dataDir, base) {
		return "", ErrNotFound
	}
	target, err := createThemeDir(dataDir, newName)
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(target)
		}
	}()
	if IsBuiltin(base) {
		err = copyBuiltin(target)
	} else {
		var source string
		source, err = themeDir(dataDir, base)
		if err == nil {
			err = copyTree(source, target)
		}
	}
	if err != nil {
		return "", err
	}
	complete = true
	return target, nil
}

func Delete(dataDir, name string) error {
	if IsBuiltin(name) {
		return ErrReadOnly
	}
	dir, err := themeDir(dataDir, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("检查服务器主题: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除服务器主题: %w", err)
	}
	return nil
}

func themeDir(dataDir, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "console-themes", name), nil
}

func createThemeDir(dataDir, name string) (string, error) {
	dir, err := themeDir(dataDir, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return "", fmt.Errorf("创建服务器主题根目录: %w", err)
	}
	if err := os.Mkdir(dir, 0o750); errors.Is(err, fs.ErrExist) {
		return "", ErrExists
	} else if err != nil {
		return "", fmt.Errorf("创建服务器主题目录: %w", err)
	}
	return dir, nil
}

func copyBuiltin(target string) error {
	return fs.WalkDir(builtinAssets, "assets/default", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, "assets/default/")
		content, err := builtinAssets.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取内置服务器主题: %w", err)
		}
		return writeFile(target, rel, content)
	})
}

func copyTree(source, target string) error {
	count, size, err := dirStats(source)
	if err != nil {
		return fmt.Errorf("检查来源服务器主题: %w", err)
	}
	if count > maxEntries || size > maxTreeBytes {
		return errors.New("来源服务器主题超出复制限制")
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("来源服务器主题包含符号链接")
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(target, filepath.ToSlash(rel), content)
	})
}

func builtinStats() (int, int64) {
	count := 0
	var size int64
	_ = fs.WalkDir(builtinAssets, "assets/default", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, readErr := builtinAssets.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		count++
		size += int64(len(content))
		return nil
	})
	return count, size
}

func dirStats(dir string) (int, int64, error) {
	count := 0
	var size int64
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		count++
		size += info.Size()
		return nil
	})
	return count, size, err
}

