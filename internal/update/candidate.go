// 更新候选
package update

import (
	"fmt"
	"strings"
	"time"

	"github.com/oss/oss-server/internal/version"
)

// Candidate 表示一次可用的更新候选，来源于 GitHub Release。
type Candidate struct {
	Version     string `json:"version"`      // 规范化版本（无 v 前缀）
	Tag         string `json:"tag"`          // 原始 tag（含 v 前缀如有）
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	AssetName   string `json:"asset_name"`
	AssetURL    string `json:"asset_url"`
	ReleaseURL  string `json:"release_url"`
	Size        int64  `json:"size"`
	PublishedAt string `json:"published_at,omitempty"`
	ReleaseID   int64  `json:"release_id"`
	AssetID     int64  `json:"asset_id"`
	Digest      string `json:"digest"` // sha256:<64 hex>
}

// Operation 表示一次更新操作的记录。
type Operation struct {
	ID         string         `json:"id"`
	State      OperationState `json:"state"`
	Candidate  *Candidate     `json:"candidate,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	Error      string         `json:"error,omitempty"`
	BackupPath string         `json:"backup_path,omitempty"`
}

// IsTerminal 判断操作是否已进入终态（成功/失败/已是最新）。
func (o Operation) IsTerminal() bool {
	return o.State.IsTerminal()
}

// NewCandidate 为唯一的生产构造函数，要求真实的 release/asset ID 与 sha256 digest。
func NewCandidate(tag, goos, goarch, assetURL, releaseURL string, size int64, releaseID, assetID int64, digest string) (*Candidate, error) {
	if tag == "" {
		return nil, newUpdateError(CodeInvalidVersion, "tag is empty", ErrInvalidVersion)
	}
	normTag := version.Normalize(tag)
	if normTag == "" {
		return nil, newUpdateError(CodeInvalidVersion, "tag is empty after normalization", ErrInvalidVersion)
	}
	sv, err := version.Parse(normTag)
	if err != nil {
		return nil, newUpdateError(CodeInvalidVersion, fmt.Sprintf("invalid version %q", tag), err)
	}
	if sv.Prerelease != "" {
		return nil, newUpdateError(CodeInvalidVersion, fmt.Sprintf("prerelease version %q not allowed", tag), ErrInvalidVersion)
	}
	if !IsSupportedPlatform(goos, goarch) {
		return nil, newUpdateError(CodeUnsupportedPlatform, fmt.Sprintf("unsupported platform %s/%s", goos, goarch), ErrUnsupportedPlatform)
	}
	assetName, err := AssetName(normTag, goos, goarch)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, newUpdateError(CodeInvalidSize, fmt.Sprintf("invalid size %d", size), ErrInvalidSize)
	}
	if size > maxDownloadSize {
		return nil, newUpdateError(CodeInvalidSize, fmt.Sprintf("size %d exceeds limit %d", size, maxDownloadSize), ErrInvalidSize)
	}
	if !isValidAssetURL(assetURL) {
		return nil, newUpdateError(CodeInvalidURL, fmt.Sprintf("asset_url must be https, got %q", assetURL), ErrInvalidURL)
	}
	if !isHTTPSURL(assetURL) && !isLoopbackURL(assetURL) {
		return nil, newUpdateError(CodeInvalidURL, fmt.Sprintf("asset_url must be https, got %q", assetURL), ErrInvalidURL)
	}
	if !isValidAssetURL(releaseURL) {
		return nil, newUpdateError(CodeInvalidURL, fmt.Sprintf("release_url must be https, got %q", releaseURL), ErrInvalidURL)
	}
	if !isHTTPSURL(releaseURL) && !isLoopbackURL(releaseURL) {
		return nil, newUpdateError(CodeInvalidURL, fmt.Sprintf("release_url must be https, got %q", releaseURL), ErrInvalidURL)
	}
	if releaseID <= 0 {
		return nil, newUpdateError(CodeInvalidAsset, fmt.Sprintf("release_id must be positive, got %d", releaseID), ErrInvalidAsset)
	}
	if assetID <= 0 {
		return nil, newUpdateError(CodeInvalidAsset, fmt.Sprintf("asset_id must be positive, got %d", assetID), ErrInvalidAsset)
	}
	if !isValidDigest(digest) {
		return nil, newUpdateError(CodeInvalidAsset, fmt.Sprintf("digest %q is missing or malformed, want sha256:<64 hex>", digest), ErrInvalidAsset)
	}
	c := &Candidate{
		Version:    normTag,
		Tag:        tag,
		GOOS:       goos,
		GOARCH:     goarch,
		AssetName:  assetName,
		AssetURL:   assetURL,
		ReleaseURL: releaseURL,
		Size:       size,
		ReleaseID:  releaseID,
		AssetID:    assetID,
		Digest:     strings.ToLower(digest),
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate 严格校验候选的完整性：版本、资产名精确匹配、大小、HTTPS URL、不可变 ID 与 digest。
func (c Candidate) Validate() error {
	norm := version.Normalize(c.Version)
	if norm == "" {
		return newUpdateError(CodeInvalidVersion, "candidate version is empty", ErrInvalidVersion)
	}
	sv, err := version.Parse(norm)
	if err != nil {
		return newUpdateError(CodeInvalidVersion, fmt.Sprintf("invalid candidate version %q", c.Version), err)
	}
	if sv.Prerelease != "" {
		return newUpdateError(CodeInvalidVersion, fmt.Sprintf("candidate version %q must be stable, prerelease not allowed", c.Version), ErrInvalidVersion)
	}
	if norm != c.Version {
		return newUpdateError(CodeInvalidVersion, fmt.Sprintf("candidate version %q must be normalized without v prefix", c.Version), ErrInvalidVersion)
	}
	if c.GOOS == "" || c.GOARCH == "" {
		return newUpdateError(CodeUnsupportedPlatform, "candidate platform is empty", ErrUnsupportedPlatform)
	}
	if !IsSupportedPlatform(c.GOOS, c.GOARCH) {
		return newUpdateError(CodeUnsupportedPlatform, fmt.Sprintf("unsupported candidate platform %s/%s", c.GOOS, c.GOARCH), ErrUnsupportedPlatform)
	}
	expected, err := AssetName(c.Version, c.GOOS, c.GOARCH)
	if err != nil {
		return err
	}
	if c.AssetName != expected {
		return newUpdateError(CodeInvalidAsset, fmt.Sprintf("asset name %q does not match expected %q for %s/%s", c.AssetName, expected, c.GOOS, c.GOARCH), ErrInvalidAsset)
	}
	if c.Size <= 0 {
		return newUpdateError(CodeInvalidSize, fmt.Sprintf("candidate size must be positive, got %d", c.Size), ErrInvalidSize)
	}
	if c.Size > maxDownloadSize {
		return newUpdateError(CodeInvalidSize, fmt.Sprintf("candidate size %d exceeds limit %d", c.Size, maxDownloadSize), ErrInvalidSize)
	}
	if !isValidAssetURL(c.AssetURL) {
		return newUpdateError(CodeInvalidURL, fmt.Sprintf("asset_url must be https, got %q", c.AssetURL), ErrInvalidURL)
	}
	if !isHTTPSURL(c.AssetURL) && !isLoopbackURL(c.AssetURL) {
		return newUpdateError(CodeInvalidURL, fmt.Sprintf("asset_url must be https, got %q", c.AssetURL), ErrInvalidURL)
	}
	if !isValidAssetURL(c.ReleaseURL) {
		return newUpdateError(CodeInvalidURL, fmt.Sprintf("release_url must be https, got %q", c.ReleaseURL), ErrInvalidURL)
	}
	if !isHTTPSURL(c.ReleaseURL) && !isLoopbackURL(c.ReleaseURL) {
		return newUpdateError(CodeInvalidURL, fmt.Sprintf("release_url must be https, got %q", c.ReleaseURL), ErrInvalidURL)
	}
	if c.ReleaseID <= 0 {
		return newUpdateError(CodeInvalidAsset, fmt.Sprintf("release_id must be positive, got %d", c.ReleaseID), ErrInvalidAsset)
	}
	if c.AssetID <= 0 {
		return newUpdateError(CodeInvalidAsset, fmt.Sprintf("asset_id must be positive, got %d", c.AssetID), ErrInvalidAsset)
	}
	if !isValidDigest(c.Digest) {
		return newUpdateError(CodeInvalidAsset, fmt.Sprintf("digest %q is missing or malformed, want sha256:<64 hex>", c.Digest), ErrInvalidAsset)
	}
	// Normalize digest to lowercase for stable comparison
	if c.Digest != strings.ToLower(c.Digest) {
		return newUpdateError(CodeInvalidAsset, fmt.Sprintf("digest %q must be lowercase", c.Digest), ErrInvalidAsset)
	}
	return nil
}

