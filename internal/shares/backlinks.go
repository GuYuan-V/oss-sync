package shares

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/models"
)

// wikilinkRe 匹配 [[目标]] 与 [[目标|别名]] 形式的 wiki 链接
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]\|]+)(?:\|[^\[\]]+)?\]\]`)

// collectBacklinks 读取源文件正文，解析其中 wiki 链接指向的目标，
// 同一目标去重后按库内相对路径返回，源文件不存在时返回错误
func (h *Handler) collectBacklinks(userID uint, vaultID, sourcePath string) ([]string, error) {
	var f models.File
	if err := h.DB.Where(
		"user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ? AND type = ?",
		userID, vaultID, sourcePath, false, "markdown",
	).First(&f).Error; err != nil {
		return nil, fmt.Errorf("source file not found: %s", sourcePath)
	}

	abs := filestore.DiskPath(h.Cfg.Storage.DataDir, f)
	raw, err := readAbsFile(abs)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var out []string
	for _, m := range wikilinkRe.FindAllStringSubmatch(string(raw), -1) {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		candidate := name
		if !strings.HasSuffix(strings.ToLower(candidate), ".md") {
			candidate = candidate + ".md"
		}
		if _, dup := seen[candidate]; dup {
			continue
		}
		var matches []models.File
		h.DB.Where(
			"user_id = ? AND vault_id = ? AND is_deleted = ? AND type = ? AND path LIKE ?",
			userID, vaultID, false, "markdown", "%/"+candidate,
		).
			Or(
				"user_id = ? AND vault_id = ? AND is_deleted = ? AND type = ? AND path = ?",
				userID, vaultID, false, "markdown", candidate,
			).
			Find(&matches)
		if len(matches) == 0 {
			continue
		}
		chosen := matches[0]
		for _, mf := range matches {
			if mf.MTime > chosen.MTime {
				chosen = mf
			}
		}
		norm := normalizeRelPath(chosen.Path)
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out, nil
}

// isSafeSharePath 判断分享路径是否为库内相对路径，拒绝绝对路径、上级目录与盘符前缀
func isSafeSharePath(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return false
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.HasPrefix(clean, "../") {
		return false
	}
	if strings.Contains(p, ":") && !strings.HasSuffix(p, ":") {
		return false
	}
	return true
}

// normalizeRelPath 将反斜杠路径统一为斜杠并清理为规范的库内相对路径
func normalizeRelPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return path.Clean(p)
}

// readAbsFile 读取磁盘文件，作为可注入的读文件入口便于测试替换
var readAbsFile = func(p string) ([]byte, error) {
	return readFileBytes(p)
}

// readFileBytes 读取整个文件内容
func readFileBytes(p string) ([]byte, error) {
	return os.ReadFile(p)
}
