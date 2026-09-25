// Package update 提供发布检查、下载校验、服务端二进制原子替换，以及 helper 交接与回滚
// 更新仅由管理员触发，不执行后台轮询
package update

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/version"
)

// Options 覆盖 Updater 默认值，主要供可重复测试使用
type Options struct {
	// GitHubRepo 覆盖配置中的 owner 与 repo
	GitHubRepo string
	// GitHubToken 覆盖 OSS_GITHUB_TOKEN
	GitHubToken string
	// APIBase 覆盖 GitHub API 地址
	APIBase string
	// DownloadSource 选择 official、proxy 或 custom 下载源
	DownloadSource string
	// DownloadProxy 覆盖自定义 HTTPS 前缀
	DownloadProxy string
	// ExecPath 覆盖可执行文件路径，空值表示使用 os.Executable
	ExecPath string
	// HTTPClient 覆盖发布元数据请求所用的客户端
	HTTPClient *http.Client
	// Verifier 校验下载二进制与其报告的版本
	Verifier func(path, wantVersion string) error
	// Pin 将更新限定到单个规范化后的发布 tag
	Pin string
	// SkipTLSVerify 允许受控测试中使用自签名 HTTPS 端点
	SkipTLSVerify bool
}

// Updater 持有更新状态机，同一时刻仅允许一次更新运行
type Updater struct {
	gh           *GitHubClient
	exe          string
	backup       string
	verifier     func(path, wantVersion string) error
	running      atomic.Bool
	restartFired atomic.Bool

	stateMu     sync.Mutex // 保护 lastCheck 与 lastUpdate 快照
	lastCheck   *CheckResult
	lastUpdate  *UpdateResult
	updatePhase OperationState
	pinned      string
	source      string
	proxy       string

	onUpdated func()
}

const (
	updatePhaseIdle         = StateIdle
	updatePhasePrepare      = StatePrepare
	updatePhaseFetchRelease = StateFetchRelease
	updatePhaseSelectAsset  = StateSelectAsset
	updatePhaseDownload     = StateDownload
	updatePhaseVerify       = StateVerify
	updatePhaseBackup       = StateBackup
	updatePhaseSwap         = StateSwap
	updatePhaseDone         = StateDone
	updatePhaseFailed       = StateFailed
	updatePhaseUpToDate     = StateUpToDate
)

// NewUpdater 创建更新器并解析可执行文件路径
func NewUpdater(cfg *config.Config, opts ...Options) (*Updater, error) {
	opt := Options{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	httpClient := opt.HTTPClient
	if httpClient == nil {
		httpClient = defaultUpdaterHTTPClient(cfg.Update.EffectiveTimeout(), opt.SkipTLSVerify)
	}
	exe := opt.ExecPath
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("无法确定当前可执行文件路径: %w", err)
		}
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return nil, err
	}
	ghCfg := cfg
	if opt.GitHubRepo != "" {
		ghCfg = cloneWithRepo(cfg, opt.GitHubRepo)
	}
	verifier := opt.Verifier
	if verifier == nil {
		verifier = defaultVerifier
	}
	gh := newGitHubClient(ghCfg, httpClient)
	if opt.APIBase != "" {
		gh.apiBase = opt.APIBase
	}
	source := cfg.Update.EffectiveDownloadSource()
	proxy := cfg.Update.EffectiveDownloadProxy()
	if opt.DownloadSource != "" {
		source = opt.DownloadSource
	}
	if opt.DownloadProxy != "" {
		proxy = opt.DownloadProxy
	}
	return &Updater{
		gh:          gh,
		exe:         abs,
		backup:      abs + ".bak",
		verifier:    verifier,
		pinned:      normalizeVersion(opt.Pin),
		updatePhase: updatePhaseIdle,
		source:      source,
		proxy:       proxy,
	}, nil
}

func defaultUpdaterHTTPClient(timeout time.Duration, skipTLSVerify bool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if skipTLSVerify {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.InsecureSkipVerify = true
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func cloneWithRepo(cfg *config.Config, repo string) *config.Config {
	cp := *cfg
	cp.Update = cfg.Update
	cp.Update.GitHubRepo = repo
	return &cp
}

// SetOnUpdated 注册请求优雅重启的一次性回调
func (u *Updater) SetOnUpdated(fn func()) {
	u.onUpdated = fn
}

// TriggerRestart 在响应可写后异步触发重启回调，并抑制重复触发
func (u *Updater) TriggerRestart() {
	if u.onUpdated == nil || !u.restartFired.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		u.onUpdated()
	}()
}

// CheckUpdate 比较当前版本与最新 Release，并缓存检查结果
func (u *Updater) CheckUpdate(ctx context.Context) (*CheckResult, error) {
	release, err := u.gh.fetchLatestFrom(ctx, u.source, u.proxy)
	if err != nil && !errors.Is(err, ErrNoRelease) {
		return nil, err
	}
	current := version.Version
	res := &CheckResult{
		CheckedAt:      time.Now().UTC(),
		CurrentVersion: current,
	}
	if err == nil {
		res.LatestVersion = release.TagName
		res.ReleaseURL = release.HTMLURL
		if isReleaseNewerThanCurrent(release.TagName, current) {
			res.UpdateAvailable = true
		}
	} else {
		res.Note = "上游仓库暂无 Release"
	}
	u.stateMu.Lock()
	cp := *res
	u.lastCheck = &cp
	u.stateMu.Unlock()
	// 返回与存储快照不同的副本，保持不可变语义
	rcp := *res
	return &rcp, nil
}

func isReleaseNewerThanCurrent(releaseTag, current string) bool {
	if version.IsDevelopmentVersion(current) {
		// 任意合法稳定 Release 均视为比 dev 新
		if _, err := version.Parse(releaseTag); err == nil {
			sv, _ := version.Parse(releaseTag)
			if sv.Prerelease == "" {
				return true
			}
		}
		return false
	}
	cmp, err := version.Compare(releaseTag, current)
	if err != nil {
		return false
	}
	return cmp > 0
}

// Update 执行发布选择、下载校验、备份与原子替换
// 何时调用 TriggerRestart 由调用方在成功响应后决定
func (u *Updater) Update(ctx context.Context) *UpdateResult {
	if !u.running.CompareAndSwap(false, true) {
		return &UpdateResult{
			At:    time.Now().UTC(),
			Code:  "in_progress",
			Phase: StateInProgress,
			State: StateInProgress,
			Error: "更新已在进行中",
		}
	}
	defer u.running.Store(false)

	res := &UpdateResult{At: time.Now().UTC()}
	u.setUpdateResultPhase(res, updatePhasePrepare)

	release, err := u.gh.fetchLatestFrom(ctx, u.source, u.proxy)
	u.setUpdateResultPhase(res, updatePhaseFetchRelease)
	if err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "github_error"
		res.Error = err.Error()
		return res
	}
	sv, err := version.Parse(release.TagName)
	if err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "github_error"
		res.Error = fmt.Sprintf("Release tag 非法 %q: %v", release.TagName, err)
		return res
	}
	if sv.Prerelease != "" {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "github_error"
		res.Error = fmt.Sprintf("Release tag %q 为 prerelease，不允许", release.TagName)
		return res
	}
	latest := version.Normalize(release.TagName)
	// 额外校验：latest 必须与 TagName 规范化后一致且无 prerelease
	if latest == "" {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "github_error"
		res.Error = "Release tag 为空"
		return res
	}
	res.Version = latest
	if u.pinned != "" && latest != u.pinned {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "failed"
		res.Error = fmt.Sprintf("tag 不匹配：要求 %q，实际 %q", u.pinned, latest)
		return res
	}
	if !isReleaseNewerThanCurrent(release.TagName, version.Version) {
		u.setUpdateResultPhase(res, updatePhaseUpToDate)
		res.Code = "up_to_date"
		res.Error = fmt.Sprintf("当前已是最新版本 %s", version.Version)
		return res
	}
	res.Version = latest

	u.setUpdateResultPhase(res, updatePhaseSelectAsset)
	asset, err := selectAsset(release.Assets, release.TagName, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "failed"
		res.Error = err.Error()
		return res
	}
	assetURL := asset.BrowserDownloadURL
	if assetURL == "" {
		assetURL = asset.URL
	}
	resolvedAssetURL, err := resolveUpdateURL(assetURL, u.source, u.proxy)
	if err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "failed"
		res.Error = err.Error()
		return res
	}
	if asset.BrowserDownloadURL != "" {
		asset.BrowserDownloadURL = resolvedAssetURL
	} else {
		asset.URL = resolvedAssetURL
	}

	tmpDir, err := os.MkdirTemp(filepath.Dir(u.exe), ".oss-update-*")
	if err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "failed"
		res.Error = fmt.Sprintf("创建临时目录失败: %v", err)
		return res
	}
	defer os.RemoveAll(tmpDir)

	u.setUpdateResultPhase(res, updatePhaseDownload)
	prepared, err := u.downloadAsset(ctx, *asset, tmpDir)
	if err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "failed"
		res.Error = err.Error()
		return res
	}

	u.setUpdateResultPhase(res, updatePhaseVerify)
	if u.verifier != nil {
		if err := u.verifier(prepared, latest); err != nil {
			u.setUpdateResultPhase(res, updatePhaseFailed)
			res.Code = "failed"
			res.Error = fmt.Sprintf("下载校验失败: %v", err)
			return res
		}
	}

	u.setUpdateResultPhase(res, updatePhaseBackup)
	if err := copyFile(u.exe, u.backup); err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "failed"
		res.Error = fmt.Sprintf("备份当前二进制失败: %v", err)
		return res
	}
	res.BackupPath = u.backup

	u.setUpdateResultPhase(res, updatePhaseSwap)
	if err := swapBinary(prepared, u.exe); err != nil {
		u.setUpdateResultPhase(res, updatePhaseFailed)
		res.Code = "failed"
		res.Error = fmt.Sprintf("替换二进制失败: %v", err)
		return res
	}

	// 写入“更新待验证”标记：重启后由 StartupHealthCheck 轮询 /readyz，
	// 未就绪时据此回滚到备份二进制；标记写入失败不影响更新本身
	if err := os.WriteFile(u.exe+".updated", []byte(latest), 0o644); err != nil {
		log.Printf("[OSS] 写入更新待验证标记失败: %v", err)
	}

	res.OK = true
	res.Code = "ok"
	u.setUpdateResultPhase(res, updatePhaseDone)
	return res
}

func (u *Updater) setUpdateResultPhase(res *UpdateResult, phase OperationState) {
	u.stateMu.Lock()
	res.Phase = phase
	res.State = phase
	cp := *res
	u.lastUpdate = &cp
	u.updatePhase = phase
	u.stateMu.Unlock()
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// ExecPath 返回当前二进制路径
func (u *Updater) ExecPath() string { return u.exe }

// BackupPath 返回备份文件路径
func (u *Updater) BackupPath() string { return u.backup }
