// 更新下载
package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/oss/oss-server/internal/version"
)

// maxDownloadSize 限制单个资产下载上限，防止异常数据撑爆磁盘。
const maxDownloadSize = 512 << 20 // 512 MiB

// downloadAsset 下载资产；压缩包会解包并返回其中匹配当前平台的可执行文件。
// 在解包/魔数校验之前必须先通过 SHA-256 完整性校验。
func (u *Updater) downloadAsset(ctx context.Context, asset Asset, dir string) (string, error) {
	if asset.BrowserDownloadURL == "" && asset.URL == "" {
		return "", fmt.Errorf("资产 %q 没有下载地址", asset.Name)
	}
	// 优先使用 BrowserDownloadURL，其次为 API URL（通过 Accept 头请求二进制）。
	downloadURL := asset.BrowserDownloadURL
	if downloadURL == "" {
		downloadURL = asset.URL
	}
	if !allowedDownloadURL(u.gh.apiBase, downloadURL) {
		return "", fmt.Errorf("资产 %q 下载地址不是 HTTPS: %q", asset.Name, downloadURL)
	}
	if asset.Size <= 0 {
		return "", newUpdateError(CodeInvalidSize, fmt.Sprintf("asset %q size invalid %d", asset.Name, asset.Size), ErrInvalidSize)
	}
	if asset.Size > maxDownloadSize {
		return "", newUpdateError(CodeInvalidSize, fmt.Sprintf("asset %q size %d exceeds limit %d", asset.Name, asset.Size, maxDownloadSize), ErrInvalidSize)
	}
	if !isValidDigest(asset.Digest) {
		return "", newUpdateError(CodeInvalidAsset, fmt.Sprintf("asset %q digest %q malformed", asset.Name, asset.Digest), ErrInvalidAsset)
	}
	name := filepath.Base(asset.Name)
	if name == "." || name == "/" || name == "" {
		name = "oss-server.bin"
	}
	dest := filepath.Join(dir, name)
	// 使用带安全重定向策略的客户端：拒绝 downgrade、跨 host 时剥离 Authorization。
	safeClient := clientWithSafeRedirect(u.gh.http, u.gh.apiBase)
	// Authorization 仅附加到已配置的 GitHub API 资产原点；browser_download_url 绝不附加。
	effectiveToken := ""
	if downloadURL != "" && downloadURL == asset.URL && shouldAttachToken(downloadURL, u.gh.apiBase) {
		effectiveToken = u.gh.token
	}
	// 若使用 BrowserDownloadURL，保持 effectiveToken 为空，即使其 host 恰好与 apiBase 相同也绝不附带。
	if downloadURL == asset.BrowserDownloadURL {
		effectiveToken = ""
	}
	if err := downloadFile(ctx, safeClient, downloadURL, dest, asset.Size, asset.Digest, effectiveToken, u.gh.apiBase); err != nil {
		return "", fmt.Errorf("下载 %q 失败: %w", asset.Name, err)
	}
	if isArchive(name) {
		exe, err := extractBinary(dest, dir)
		if err != nil {
			return "", fmt.Errorf("解包 %q 失败: %w", asset.Name, err)
		}
		return exe, nil
	}
	return dest, nil
}

// allowedDownloadURL 只允许 HTTPS 下载；测试中允许指向与 API base 相同来源或 loopback 的 http 地址。
func allowedDownloadURL(apiBase, raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme == "http" {
		if isLoopbackURL(raw) {
			return true
		}
		base, err := url.Parse(apiBase)
		if err == nil && base.Host != "" && u.Host == base.Host {
			return true
		}
	}
	return false
}

func clientWithSafeRedirect(base *http.Client, apiBase string) *http.Client {
	if base == nil {
		base = &http.Client{}
	}
	clone := *base
	origCheck := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		prev := via[len(via)-1]
		// 拒绝 https -> http downgrade
		if prev.URL.Scheme == "https" && req.URL.Scheme == "http" {
			return errors.New("redirect downgrade from https to http rejected")
		}
		// 拒绝重定向到非 https（除非 loopback 或同主机 http 兼容测试）
		if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
			return errors.New("redirect to non-http(s) url rejected")
		}
		if req.URL.Scheme == "http" && !isLoopbackURL(req.URL.String()) {
			baseURL, err := url.Parse(apiBase)
			if err != nil || req.URL.Host != baseURL.Host {
				if prev.URL.Scheme == "https" {
					return errors.New("redirect to http rejected")
				}
				// 对 http->http 跨 host 仍剥离 token，但允许重定向本身
			}
		}
		// 跨 host 时剥离 Authorization，避免 token 泄露
		if prev.URL.Host != req.URL.Host {
			req.Header.Del("Authorization")
		}
		if origCheck != nil {
			return origCheck(req, via)
		}
		return nil
	}
	return &clone
}

// shouldAttachToken 判断是否应对 rawURL 附加 Authorization。仅当 rawURL 的 host 与 apiBase 的 host 完全一致时才附加，
// 且此判断由调用方的 apiBase 决定；browser_download_url 场景调用方应传入空 token（见 downloadAsset）。
func shouldAttachToken(rawURL, apiBase string) bool {
	if strings.TrimSpace(rawURL) == "" || strings.TrimSpace(apiBase) == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(apiBase))
	if err != nil || base.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, base.Host)
}

// downloadFile 把 URL 下载到 dest，校验 Content-Length、声明大小、上限与 SHA-256。
func downloadFile(ctx context.Context, client *http.Client, rawURL, dest string, expected int64, expectedDigest string, token string, apiBase string) error {
	if expected <= 0 {
		return newUpdateError(CodeInvalidSize, fmt.Sprintf("expected size must be positive, got %d", expected), ErrInvalidSize)
	}
	if expected > maxDownloadSize {
		return newUpdateError(CodeInvalidSize, fmt.Sprintf("expected size %d exceeds limit %d", expected, maxDownloadSize), ErrInvalidSize)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "oss-sync-updater/"+version.Version)
	// 若下载地址为 GitHub API 资产 endpoint，需要 Accept: application/octet-stream
	if strings.Contains(rawURL, "/releases/assets/") {
		req.Header.Set("Accept", "application/octet-stream")
	}
	if token != "" && shouldAttachToken(rawURL, apiBase) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	if expected > 0 && resp.ContentLength > 0 && resp.ContentLength != expected {
		return fmt.Errorf("大小不匹配: 期望 %d 字节，实际 %d", expected, resp.ContentLength)
	}
	// ContentLength 为 -1 时也需通过上限校验
	max := expected
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, max+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > max {
		return fmt.Errorf("超过下载上限 %d 字节", max)
	}
	if written != expected {
		return fmt.Errorf("下载不完整: 期望 %d 字节，实际 %d", expected, written)
	}
	if expectedDigest != "" {
		if err := verifyFileDigest(dest, expectedDigest); err != nil {
			return err
		}
	}
	return syncErr
}

func verifyFileDigest(path, digest string) error {
	if !isValidDigest(digest) {
		return newUpdateError(CodeInvalidAsset, fmt.Sprintf("digest %q malformed", digest), ErrInvalidAsset)
	}
	hexPart := strings.TrimPrefix(digest, "sha256:")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算 SHA-256 失败: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, hexPart) {
		return newUpdateError(CodeInvalidAsset, fmt.Sprintf("sha256 mismatch: got %s want %s", got, hexPart), ErrInvalidAsset)
	}
	return nil
}

func isArchive(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".tar.gz") ||
		strings.HasSuffix(l, ".tgz") ||
		strings.HasSuffix(l, ".zip")
}

// extractBinary 从压缩包中找出匹配当前平台的可执行文件。
func extractBinary(archivePath, destDir string) (string, error) {
	var candidates []string
	var err error
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		candidates, err = zipCandidates(archivePath, destDir)
	} else {
		candidates, err = tarGzCandidates(archivePath, destDir)
	}
	if err != nil {
		return "", err
	}
	return pickBinary(candidates)
}

func tarGzCandidates(archivePath, destDir string) ([]string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var candidates []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size == 0 || hdr.Size > maxDownloadSize {
			continue
		}
		tmp := filepath.Join(destDir, "x-"+filepath.Base(hdr.Name))
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return nil, err
		}
		written, copyErr := io.CopyN(out, tr, hdr.Size)
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil || written != hdr.Size {
			continue
		}
		if checkExecutableMagic(tmp, runtime.GOOS) == nil {
			candidates = append(candidates, tmp)
		}
	}
	return candidates, nil
}

func zipCandidates(archivePath, destDir string) ([]string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var candidates []string
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() || zf.FileInfo().Size() == 0 || zf.FileInfo().Size() > maxDownloadSize {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		tmp := filepath.Join(destDir, "x-"+filepath.Base(zf.Name))
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return nil, err
		}
		written, copyErr := io.CopyN(out, rc, zf.FileInfo().Size())
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil || closeErr != nil || written != zf.FileInfo().Size() {
			continue
		}
		if checkExecutableMagic(tmp, runtime.GOOS) == nil {
			candidates = append(candidates, tmp)
		}
	}
	return candidates, nil
}

// pickBinary 从候选文件中选出最可能是服务端二进制的文件。
func pickBinary(candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", errors.New("压缩包内没有匹配当前平台的可执行文件")
	}
	sort.Slice(candidates, func(i, j int) bool {
		pi, pj := prefersBinaryName(candidates[i]), prefersBinaryName(candidates[j])
		if pi != pj {
			return pi
		}
		si, _ := os.Stat(candidates[i])
		sj, _ := os.Stat(candidates[j])
		if si != nil && sj != nil && si.Size() != sj.Size() {
			return si.Size() > sj.Size()
		}
		return candidates[i] < candidates[j]
	})
	return candidates[0], nil
}

func prefersBinaryName(p string) bool {
	l := strings.ToLower(filepath.Base(p))
	return strings.Contains(l, "oss") || strings.Contains(l, "server") || strings.Contains(l, "sync")
}

