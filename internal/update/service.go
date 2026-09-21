package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/version"
)

// Service 串联 Manager 已校验候选的下载与 helper 交接
type Service struct {
	mgr *Manager
	up  *Updater
	cfg *config.Config

	onShutdown    func()
	shutdownFired atomic.Bool
}

// NewService 创建 Service，mgr 与 up 均不得为空
func NewService(mgr *Manager, up *Updater, cfg *config.Config) *Service {
	return &Service{mgr: mgr, up: up, cfg: cfg}
}

// Manager 返回持久化的管理器
func (s *Service) Manager() *Manager { return s.mgr }

// SetOnShutdown 注入交接调用成功返回后异步执行的关闭回调
func (s *Service) SetOnShutdown(fn func()) {
	s.onShutdown = fn
	s.shutdownFired.Store(false)
}

func (s *Service) triggerShutdown() {
	if s.onShutdown == nil || !s.shutdownFired.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.onShutdown()
	}()
}

// StartHelperUpdate 校验已检查候选、下载精确资产、暂存并交接给 helper
// InitiateHelperHandoff 返回 nil 错误后请求关闭；保留可恢复标记的分支也可能返回成功
func (s *Service) StartHelperUpdate(ctx context.Context, checkID, downloadSource, customProxy string) (*Operation, error) {
	if s.mgr == nil {
		return nil, fmt.Errorf("manager is nil")
	}
	if s.up == nil {
		return nil, fmt.Errorf("updater is nil")
	}
	if checkID == "" {
		return nil, newUpdateError(CodeCheckNotFound, "check_id is empty", ErrCheckNotFound)
	}
	cand, err := s.mgr.ValidateChecked(checkID)
	if err != nil {
		return nil, err
	}
	if err := cand.Validate(); err != nil {
		return nil, err
	}
	// 变更前先做能力检查
	if err := CheckHandoffCapability(s.up.exe); err != nil {
		return nil, err
	}
	downloadSource, customProxy = s.effectiveSource(downloadSource, customProxy)
	downloadURL, err := resolveDownloadURL(cand.AssetURL, downloadSource, customProxy)
	if err != nil {
		return nil, err
	}
	// 把候选资产下载到临时目录
	tmpDir, err := os.MkdirTemp(filepath.Dir(s.up.exe), ".oss-download-*")
	if err != nil {
		return nil, fmt.Errorf("create download dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	asset := Asset{
		ID:                 cand.AssetID,
		Name:               cand.AssetName,
		BrowserDownloadURL: downloadURL,
		Size:               cand.Size,
		Digest:             cand.Digest,
	}
	prepared, err := s.up.downloadAsset(ctx, asset, tmpDir)
	if err != nil {
		return nil, err
	}
	// downloadAsset 已在解包前校验发布资产的 digest；暂存可执行文件的字节与压缩包不同，
	// 此处重新计算其 digest，供暂存阶段与 helper 发现后续篡改
	binaryDigest, err := fileDigest(prepared)
	if err != nil {
		return nil, fmt.Errorf("hash prepared executable: %w", err)
	}
	readyURL := s.readyURL()
	origArgs := os.Args
	workDir, _ := os.Getwd()
	op, err := s.up.InitiateHelperHandoff(s.mgr, checkID, prepared, binaryDigest, readyURL, origArgs, workDir)
	if err != nil {
		return nil, err
	}
	s.triggerShutdown()
	return op, nil
}

func (s *Service) readyURL() string {
	if s.cfg == nil {
		return "http://127.0.0.1:8080/readyz"
	}
	host := s.cfg.Server.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	port := s.cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("http://%s:%d/readyz", host, port)
}

// CheckInfo 为 WebUI 检查更新的结果，携带持久化的 check_id
type CheckInfo struct {
	CheckID         string     `json:"check_id"`
	Candidate       *Candidate `json:"candidate"`
	CurrentVersion  string     `json:"current_version"`
	LatestVersion   string     `json:"latest_version"`
	UpdateAvailable bool       `json:"update_available"`
	ReleaseURL      string     `json:"release_url"`
	ExpiresAt       int64      `json:"expires_at"`
	Note            string     `json:"note,omitempty"`
}

// Check 按配置的更新源检查最新 Release，严格校验平台资产并颁发持久化的 check_id
// 其校验逻辑与 /api/admin/update/check 一致，返回可序列化的 CheckInfo
func (s *Service) Check(ctx context.Context) (*CheckInfo, error) {
	return s.CheckWithSource(ctx, "", "")
}

// CheckWithSource 按选定下载源检查最新 Release
func (s *Service) CheckWithSource(ctx context.Context, source, customProxy string) (*CheckInfo, error) {
	if s.mgr == nil {
		return nil, newUpdateError(CodeCorruptedState, "manager not initialized", ErrCorruptedState)
	}
	if s.up == nil {
		return nil, newUpdateError(CodeCorruptedState, "updater not initialized", ErrCorruptedState)
	}
	if s.up.gh == nil {
		return nil, newUpdateError(CodeCorruptedState, "github client not initialized", ErrCorruptedState)
	}
	// 调用方负责超时，此处追加 30 秒兜底
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	source, customProxy = s.effectiveSource(source, customProxy)
	release, err := s.up.gh.fetchLatestFrom(cctx, source, customProxy)
	if err != nil {
		if errors.Is(err, ErrNoRelease) {
			return &CheckInfo{
				CurrentVersion:  version.Version,
				UpdateAvailable: false,
				Note:            "上游仓库暂无 Release",
			}, nil
		}
		return nil, err
	}
	asset, err := selectAsset(release.Assets, release.TagName, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	cand, err := NewCandidate(release.TagName, runtime.GOOS, runtime.GOARCH, asset.BrowserDownloadURL, release.HTMLURL, asset.Size, release.ID, asset.ID, asset.Digest)
	if err != nil {
		return nil, err
	}
	ttl := time.Hour
	if s.cfg != nil {
		ttl = s.cfg.Update.EffectiveCheckTTL()
	}
	cc, err := s.mgr.IssueChecked(*cand, ttl)
	if err != nil {
		return nil, err
	}
	updateAvailable := false
	if isReleaseNewerThanCurrent(release.TagName, version.Version) {
		updateAvailable = true
	}
	return &CheckInfo{
		CheckID:         cc.ID,
		Candidate:       cand,
		CurrentVersion:  version.Version,
		LatestVersion:   release.TagName,
		UpdateAvailable: updateAvailable,
		ReleaseURL:      release.HTMLURL,
		ExpiresAt:       cc.ExpiresAt,
	}, nil
}

func (s *Service) effectiveSource(source, customProxy string) (string, string) {
	if source != "" || s.cfg == nil {
		return source, customProxy
	}
	return s.cfg.Update.EffectiveDownloadSource(), s.cfg.Update.EffectiveDownloadProxy()
}
