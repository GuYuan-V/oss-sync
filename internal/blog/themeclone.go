package blog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ScaffoldTheme creates an editable copy of an existing built-in or custom theme.
func ScaffoldTheme(dataDir, base, newName string) (string, error) {
	if err := ValidateThemeName(newName); err != nil {
		return "", err
	}
	if IsBuiltinTheme(newName) {
		return "", errors.New("副本名称必须与内置模板不同")
	}
	if !IsBuiltinTheme(base) && !CustomThemeExists(dataDir, base) {
		return "", errors.New("基础模板不存在")
	}

	targetDir, err := themeDirectory(dataDir, newName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o750); err != nil {
		return "", fmt.Errorf("创建主题根目录: %w", err)
	}
	if _, err := os.Stat(targetDir); err == nil {
		return "", errThemeExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("检查主题目录: %w", err)
	}
	if err := os.Mkdir(targetDir, 0o750); err != nil {
		return "", fmt.Errorf("创建主题目录: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(targetDir)
		}
	}()

	marker := "scaffolded"
	if IsBuiltinTheme(base) {
		err = copyBuiltinTheme(base, targetDir)
	} else {
		marker = "cloned"
		err = copyCustomTheme(dataDir, base, targetDir)
	}
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(targetDir, ".oss-theme-source"), []byte(marker), 0o640); err != nil {
		return "", fmt.Errorf("写入主题来源: %w", err)
	}
	complete = true
	return targetDir, nil
}

func copyBuiltinTheme(base, targetDir string) error {
	root := "assets/" + base
	if err := fs.WalkDir(themeAssetsFS, root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, root+"/")
		content, err := themeAssetsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取内置模板文件: %w", err)
		}
		return writeThemeCopy(targetDir, rel, content)
	}); err != nil {
		return fmt.Errorf("复制内置模板: %w", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, customTemplateFile)); errors.Is(err, fs.ErrNotExist) {
		content, readErr := themeAssetsFS.ReadFile("assets/development-template/" + customTemplateFile)
		if readErr != nil {
			return fmt.Errorf("读取开发模板: %w", readErr)
		}
		if writeErr := writeThemeCopy(targetDir, customTemplateFile, content); writeErr != nil {
			return writeErr
		}
	} else if err != nil {
		return fmt.Errorf("检查模板文件: %w", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "README.md")); errors.Is(err, fs.ErrNotExist) {
		content, readErr := themeAssetsFS.ReadFile("assets/scaffold/README.md")
		if readErr != nil {
			return fmt.Errorf("读取模板说明: %w", readErr)
		}
		if writeErr := writeThemeCopy(targetDir, "README.md", content); writeErr != nil {
			return writeErr
		}
	} else if err != nil {
		return fmt.Errorf("检查模板说明: %w", err)
	}
	return nil
}

func copyCustomTheme(dataDir, base, targetDir string) error {
	sourceDir, err := themeDirectory(dataDir, base)
	if err != nil {
		return err
	}
	count, size, err := dirStats(sourceDir)
	if err != nil {
		return fmt.Errorf("检查基础模板: %w", err)
	}
	if count > maxZipEntries || size > maxThemeTreeBytes {
		return errors.New("基础模板超出复制限制")
	}
	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("基础模板包含符号链接")
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !safeThemeEntryPath(rel) {
			return fmt.Errorf("基础模板包含非法路径: %s", rel)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取基础模板文件: %w", err)
		}
		return writeThemeCopy(targetDir, rel, content)
	})
}

func writeThemeCopy(targetDir, rel string, content []byte) error {
	path := filepath.Join(targetDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("创建模板子目录: %w", err)
	}
	if err := os.WriteFile(path, content, 0o640); err != nil {
		return fmt.Errorf("写入模板文件: %w", err)
	}
	return nil
}
