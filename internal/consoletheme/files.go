// 控制台主题文件
package consoletheme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxEditableBytes = 1 << 20

var editableExtensions = map[string]bool{
	".css":  true,
	".json": true,
	".md":   true,
	".svg":  true,
	".txt":  true,
}

var servedExtensions = map[string]bool{
	".css":   true,
	".gif":   true,
	".jpeg":  true,
	".jpg":   true,
	".otf":   true,
	".png":   true,
	".ttf":   true,
	".webp":  true,
	".woff":  true,
	".woff2": true,
}

func ListFiles(dataDir, name string) ([]string, error) {
	if IsBuiltin(name) {
		return nil, ErrReadOnly
	}
	dir, err := themeDir(dataDir, name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	files := make([]string, 0, 8)
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func ReadFile(dataDir, name, rel string) ([]byte, error) {
	if IsBuiltin(name) {
		return nil, ErrReadOnly
	}
	if !editableExtensions[strings.ToLower(filepath.Ext(rel))] {
		return nil, errors.New("只允许在线读取文本主题文件")
	}
	path, err := safeFilePath(dataDir, name, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxEditableBytes {
		return nil, errors.New("服务器主题文件不是常规文件或超过 1 MiB")
	}
	return os.ReadFile(path)
}

func SaveFile(dataDir, name, rel string, content []byte) error {
	if IsBuiltin(name) {
		return ErrReadOnly
	}
	if len(content) > maxEditableBytes {
		return errors.New("服务器主题文件超过 1 MiB")
	}
	if !editableExtensions[strings.ToLower(filepath.Ext(rel))] {
		return errors.New("只允许在线编辑文本主题文件")
	}
	path, err := safeFilePath(dataDir, name, rel)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("服务器主题文件不是常规文件")
	}
	if err := os.WriteFile(path, content, 0o640); err != nil {
		return fmt.Errorf("保存服务器主题文件: %w", err)
	}
	return nil
}

func AssetPath(dataDir, name, rel string) (string, error) {
	if IsBuiltin(name) {
		return "", ErrNotFound
	}
	if !servedExtensions[strings.ToLower(filepath.Ext(rel))] {
		return "", ErrNotFound
	}
	path, err := safeFilePath(dataDir, name, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return path, nil
}

func safeFilePath(dataDir, name, rel string) (string, error) {
	if !safeEntry(rel) {
		return "", errors.New("非法服务器主题路径")
	}
	dir, err := themeDir(dataDir, name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	relative, err := filepath.Rel(dir, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return "", errors.New("非法服务器主题路径")
	}
	return path, nil
}

func safeEntry(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.ContainsRune(name, '\x00') {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(clean, "/")
}

func writeFile(root, rel string, content []byte) error {
	if !safeEntry(rel) {
		return errors.New("非法服务器主题路径")
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0o640); err != nil {
		return err
	}
	return nil
}

