// Package blog 模板管理服务：内置清单、ZIP 上传、脚手架、文件编辑、下载与删除。
package blog

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/models"
)

// BuiltinThemeNames 内置只读模板。
var BuiltinThemeNames = []string{"default", "papertrail"}

// IsBuiltinTheme reports whether the theme is a built-in read-only template.
func IsBuiltinTheme(name string) bool {
	for _, b := range BuiltinThemeNames {
		if name == b {
			return true
		}
	}
	return false
}

// ThemeSource 模板来源。
type ThemeSource string

const (
	SourceBuiltin  ThemeSource = "builtin"
	SourceUploaded ThemeSource = "uploaded"
	SourceScaffold ThemeSource = "scaffolded"
	SourceLegacy   ThemeSource = "legacy"
)

// ThemeInfo 模板目录条目。
type ThemeInfo struct {
	Name         string      `json:"name"`
	Source       ThemeSource `json:"source"`
	Creator      string      `json:"creator,omitempty"`
	CreatedAt    string      `json:"created_at,omitempty"`
	FileCount    int         `json:"file_count"`
	Size         int64       `json:"size"`
	UsedByVaults []string    `json:"used_by_vaults"`
}

// zip limits 防止解压炸弹。
const (
	maxZipTotalBytes  = 32 << 20 // 32 MiB 总解压上限
	maxZipEntryBytes  = 8 << 20  // 8 MiB 单文件上限
	maxZipEntries     = 512      // 最多文件数
	maxEditableBytes  = 1 << 20  // 1 MiB 文本编辑上限
	maxThemeTreeBytes = 64 << 20 // 64 MiB 主题目录总大小上限
	textEditMaxFiles  = 64
)

// ListThemes 列出全部模板（内置 + 自定义）。
func ListThemes(db *gorm.DB, dataDir string) ([]ThemeInfo, error) {
	out := make([]ThemeInfo, 0, len(BuiltinThemeNames)+4)

	for _, name := range BuiltinThemeNames {
		fileCount, size := builtinThemeStats(name)
		used := themesUsedByVaults(db, name)
		out = append(out, ThemeInfo{
			Name: name, Source: SourceBuiltin,
			FileCount: fileCount, Size: size, UsedByVaults: used,
		})
	}

	root := filepath.Join(dataDir, "themes")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if IsBuiltinTheme(name) {
			continue
		}
		if ValidateThemeName(name) != nil {
			continue
		}
		dir := filepath.Join(root, name)
		fileCount, size, err := dirStats(dir)
		if err != nil {
			continue
		}
		used := themesUsedByVaults(db, name)
		source := themeSourceOf(dataDir, name)
		out = append(out, ThemeInfo{
			Name: name, Source: source,
			FileCount: fileCount, Size: size, UsedByVaults: used,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source == SourceBuiltin
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func builtinThemeStats(name string) (int, int64) {
	var count int
	var size int64
	for _, f := range []string{"template.html", "style.css", "theme.js"} {
		content, err := themeAssetsFS.ReadFile("assets/" + name + "/" + f)
		if err != nil {
			continue
		}
		count++
		size += int64(len(content))
	}
	return count, size
}

func themeSourceOf(dataDir, name string) ThemeSource {
	if IsBuiltinTheme(name) {
		return SourceBuiltin
	}
	return SourceLegacy // 目录存在即视为 legacy；上传/脚手架写入来源标记文件。
}

func themesUsedByVaults(db *gorm.DB, themeName string) []string {
	var settings []models.VaultSetting
	if err := db.Where("theme_name = ?", themeName).Find(&settings).Error; err != nil {
		return nil
	}
	out := make([]string, 0, len(settings))
	for _, s := range settings {
		out = append(out, s.VaultID)
	}
	return out
}

// UploadTheme 校验并解压 ZIP 到 data/themes/<name>。
func UploadTheme(dataDir, themeName string, r io.ReaderAt, size int64) error {
	if err := ValidateThemeName(themeName); err != nil {
		return err
	}
	if IsBuiltinTheme(themeName) {
		return errors.New("不能覆盖内置模板")
	}
	if size <= 0 || size > maxZipTotalBytes {
		return fmt.Errorf("ZIP 大小超出限制（最大 %d MiB）", maxZipTotalBytes>>20)
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("无效的 ZIP: %w", err)
	}
	if len(zr.File) == 0 || len(zr.File) > maxZipEntries {
		return fmt.Errorf("ZIP 文件数不合法（1-%d）", maxZipEntries)
	}

	var totalSize int64
	entries := make([]struct {
		name    string
		content []byte
	}, 0, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if !safeThemeEntryPath(name) {
			return fmt.Errorf("ZIP 包含非法路径: %s", name)
		}
		if f.UncompressedSize64 > maxZipEntryBytes {
			return fmt.Errorf("ZIP 单文件超出上限: %s", name)
		}
		totalSize += int64(f.UncompressedSize64)
		if totalSize > maxZipTotalBytes {
			return errors.New("ZIP 解压后总大小超出限制")
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("读取 ZIP 条目失败: %w", err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxZipEntryBytes+1))
		rc.Close()
		if err != nil {
			return fmt.Errorf("读取 ZIP 条目失败: %w", err)
		}
		if len(content) > maxZipEntryBytes {
			return fmt.Errorf("ZIP 单文件超出上限: %s", name)
		}
		entries = append(entries, struct {
			name    string
			content []byte
		}{name: name, content: content})
	}
	// 必须包含 template.html。
	hasTemplate := false
	for _, e := range entries {
		if e.name == "template.html" {
			hasTemplate = true
			break
		}
	}
	if !hasTemplate {
		return errors.New("ZIP 必须包含 template.html")
	}

	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return fmt.Errorf("创建主题根目录: %w", err)
	}
	if _, err := os.Stat(dir); err == nil {
		return errThemeExists
	}
	if err := os.Mkdir(dir, 0o750); err != nil {
		return fmt.Errorf("创建主题目录: %w", err)
	}
	// 边写入边清理：失败时移除半成品。
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	for _, e := range entries {
		full := filepath.Join(dir, filepath.FromSlash(e.name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(full, e.content, 0o640); err != nil {
			return err
		}
	}
	ok = true
	return nil
}

// safeThemeEntryPath 校验 ZIP 内相对路径，禁止 ..、绝对路径、反斜杠与空段。
func safeThemeEntryPath(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.ContainsRune(name, '\x00') {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return false
	}
	return true
}

// ListThemeFiles 列出主题目录内的文件（相对路径）。
func ListThemeFiles(dataDir, themeName string) ([]string, error) {
	if IsBuiltinTheme(themeName) {
		return nil, errThemeReadOnly
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, errors.New("主题不存在")
	}
	var out []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// ReadThemeFile 读取主题内文本文件（限 1 MiB）。
func ReadThemeFile(dataDir, themeName, relPath string) ([]byte, error) {
	if IsBuiltinTheme(themeName) {
		return nil, errThemeReadOnly
	}
	abs, err := safeThemeFilePath(dataDir, themeName, relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, errors.New("文件不存在")
	}
	if !info.Mode().IsRegular() || info.Size() > maxEditableBytes {
		return nil, errors.New("不是可编辑的文本文件或文件过大")
	}
	return os.ReadFile(abs)
}

// SaveThemeFile 保存主题内文本文件（限 1 MiB）。
func SaveThemeFile(dataDir, themeName, relPath string, content []byte) error {
	if IsBuiltinTheme(themeName) {
		return errThemeReadOnly
	}
	if len(content) > maxEditableBytes {
		return errors.New("文件超过 1 MiB 编辑上限")
	}
	abs, err := safeThemeFilePath(dataDir, themeName, relPath)
	if err != nil {
		return err
	}
	if !isEditableTextFile(relPath) {
		return errors.New("只允许编辑文本文件")
	}
	return os.WriteFile(abs, content, 0o640)
}

func safeThemeFilePath(dataDir, themeName, relPath string) (string, error) {
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return "", err
	}
	if !safeThemeEntryPath(filepath.ToSlash(relPath)) {
		return "", errors.New("非法路径")
	}
	abs := filepath.Join(dir, filepath.FromSlash(relPath))
	rel, err := filepath.Rel(dir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("非法路径")
	}
	return abs, nil
}

// editableTextExts 允许在线编辑的文件扩展名。
var editableTextExts = map[string]bool{
	".html": true, ".htm": true, ".css": true, ".js": true, ".mjs": true,
	".json": true, ".md": true, ".txt": true, ".svg": true, ".yaml": true, ".yml": true,
}

func isEditableTextFile(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	return editableTextExts[ext]
}

// CreateThemeZip 将主题目录写入提供的 zip.Writer。
func CreateThemeZip(dataDir, themeName string, zw *zip.Writer) error {
	if IsBuiltinTheme(themeName) {
		return errThemeNotDownloadable
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return errors.New("主题不存在")
	}
	var total int64
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > maxThemeTreeBytes {
			return errors.New("主题目录过大，无法打包")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}

// DeleteTheme 删除未被使用的自定义模板。
func DeleteTheme(db *gorm.DB, dataDir, themeName string) ([]string, error) {
	if IsBuiltinTheme(themeName) {
		return nil, errThemeNotDeletable
	}
	used := themesUsedByVaults(db, themeName)
	if len(used) > 0 {
		return used, errThemeInUse
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return nil, nil
}

var (
	errThemeReadOnly        = errors.New("theme_read_only: 内置模板只读")
	errThemeNotDownloadable = errors.New("theme_builtin_not_downloadable: 内置模板不可下载")
	errThemeNotDeletable    = errors.New("theme_builtin_not_deletable: 内置模板不可删除")
	errThemeInUse           = errors.New("theme_in_use: 模板正在被仓库使用")
	errThemeExists          = errors.New("theme_exists: 同名模板已存在")
)

// 导出错误供 API 层返回状态码。
var (
	ErrThemeReadOnly        = errThemeReadOnly
	ErrThemeNotDownloadable = errThemeNotDownloadable
	ErrThemeNotDeletable    = errThemeNotDeletable
	ErrThemeInUse           = errThemeInUse
	ErrThemeExists          = errThemeExists
)

// ThemeErrors 提供错误码判断。
func IsThemeReadOnly(err error) bool        { return errors.Is(err, errThemeReadOnly) }
func IsThemeNotDownloadable(err error) bool { return errors.Is(err, errThemeNotDownloadable) }
func IsThemeNotDeletable(err error) bool    { return errors.Is(err, errThemeNotDeletable) }
func IsThemeInUse(err error) bool           { return errors.Is(err, errThemeInUse) }

func dirStats(dir string) (int, int64, error) {
	var count int
	var size int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		count++
		size += info.Size()
		return nil
	})
	return count, size, err
}
