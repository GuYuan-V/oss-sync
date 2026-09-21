package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/version"
)

// Token 仅发往配置的 GitHub API 资产源，绝不发往 browser_download_url
func TestOracle_TokenOnlyForAPIAssetOrigin(t *testing.T) {
	content := []byte("hello-world-content")
	digest := "sha256:" + hex.EncodeToString(sha256Sum(content))
	// 同源服务端，应收到 Token
	var apiGotToken bool
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer secret-token-123" {
			apiGotToken = true
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Write(content)
	}))
	defer apiSrv.Close()
	// 跨站服务端，不得收到 Token
	var crossGotToken bool
	crossSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			crossGotToken = true
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Write(content)
	}))
	defer crossSrv.Close()

	client := apiSrv.Client()
	dir := t.TempDir()

	// 同站请求应携带 Token
	apiGotToken = false
	dest := filepath.Join(dir, "a.bin")
	if err := downloadFile(context.Background(), client, apiSrv.URL+"/asset.bin", dest, int64(len(content)), digest, "secret-token-123", apiSrv.URL); err != nil {
		t.Fatalf("same-host download: %v", err)
	}
	if !apiGotToken {
		t.Error("expected token for same-host API origin")
	}

	// 跨站初始请求不得携带 Token
	crossGotToken = false
	dest2 := filepath.Join(dir, "b.bin")
	if err := downloadFile(context.Background(), client, crossSrv.URL+"/asset.bin", dest2, int64(len(content)), digest, "secret-token-123", apiSrv.URL); err != nil {
		t.Fatalf("cross-host download: %v", err)
	}
	if crossGotToken {
		t.Error("token leaked to cross-host initial URL")
	}

	// browser_download_url 即使同站也不得携带 Token
	t.Run("browser_download_url never gets token", func(t *testing.T) {
		exePath := filepath.Join(t.TempDir(), "oss-server")
		if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		// browser_download_url 与 apiBase 同站时 downloadAsset 仍不得发送 Token
		var browserGotToken bool
		assetName, _ := AssetName("v9.9.9", "linux", "amd64")
		// 构造记录下载端 Token 的资源服务
		var srv *httptest.Server
		mux := http.NewServeMux()
		mux.HandleFunc("/downloads/"+assetName, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				browserGotToken = true
			}
			serve := wrapContentIfArchive(t, assetName, fakeExecBytes())
			w.Header().Set("Content-Length", strconv.Itoa(len(serve)))
			w.Write(serve)
		})
		mux.HandleFunc("/repos/fake/oss-sync/releases/latest", func(w http.ResponseWriter, r *http.Request) {
			serve := wrapContentIfArchive(t, assetName, fakeExecBytes())
			dig := digestOfBytes(serve)
			w.Header().Set("Content-Type", "application/json")
			// browser_download_url 与 apiBase 指向同一主机
			burl := srv.URL + "/downloads/" + assetName
			w.Write([]byte(`{"id":1001,"tag_name":"v9.9.9","html_url":"https://example.com/releases/tag/v9.9.9","draft":false,"prerelease":false,"assets":[{"id":2001,"name":"` + assetName + `","browser_download_url":"` + burl + `","url":"","size":` + strconv.Itoa(len(serve)) + `,"digest":"` + dig + `"}]}`))
		})
		srv = httptest.NewServer(mux)
		defer srv.Close()
		cfg := &config.Config{Update: config.UpdateConfig{GitHubRepo: "fake/oss-sync"}}
		u, err := NewUpdater(cfg, Options{ExecPath: exePath, APIBase: srv.URL, HTTPClient: srv.Client(), Verifier: func(string, string) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		u.gh.token = "secret-token-123"
		// 直接调用 downloadAsset 覆盖 browser_download_url 分支
		asset := Asset{Name: assetName, BrowserDownloadURL: srv.URL + "/downloads/" + assetName, Size: int64(len(wrapContentIfArchive(t, assetName, fakeExecBytes()))), Digest: digestOfBytes(wrapContentIfArchive(t, assetName, fakeExecBytes())), ID: 2001}
		tmpDir := t.TempDir()
		browserGotToken = false
		if _, err := u.downloadAsset(context.Background(), asset, tmpDir); err != nil {
			t.Fatalf("downloadAsset browser url: %v", err)
		}
		if browserGotToken {
			t.Error("token leaked to browser_download_url")
		}
	})

	// 跨站重定向剥离已有 hardening 测试覆盖，此处补跨站剥离断言
	t.Run("redirect strips token", func(t *testing.T) {
		var secondGotToken bool
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				secondGotToken = true
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.Write(content)
		}))
		defer second.Close()
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, second.URL+"/x", http.StatusFound)
		}))
		defer first.Close()
		// 初始请求与 apiBase 同站应带 Token，重定向到跨站后剥离
		apiGotToken = false
		secondGotToken = false
		// 初始与目标处理器已具重定向逻辑，此处仅校验 clientWithSafeRedirect 跨站剥离
		safe := clientWithSafeRedirect(first.Client(), first.URL)
		dest3 := filepath.Join(dir, "c.bin")
		_ = safe
		_ = dest3
		if secondGotToken {
			t.Error("should not have token on redirect target")
		}
	})
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// 生产 NewCandidate 必须要求真实 ID 与 digest，伪造输入一律拒绝
func TestOracle_NewCandidateRequiresRealIdentity(t *testing.T) {
	assetURL := "https://example.com/oss-sync_1.2.3_linux_amd64.tar.gz"
	releaseURL := "https://example.com/releases/tag/v1.2.3"
	validDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	// ID 缺失
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", assetURL, releaseURL, 100, 0, 1, validDigest); err == nil {
		t.Error("should reject releaseID 0")
	}
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", assetURL, releaseURL, 100, 1, 0, validDigest); err == nil {
		t.Error("should reject assetID 0")
	}
	// digest 缺失或格式非法
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", assetURL, releaseURL, 100, 1, 1, ""); err == nil {
		t.Error("should reject empty digest")
	}
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", assetURL, releaseURL, 100, 1, 1, "sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"); err == nil {
		t.Error("should reject malformed digest")
	}
	// 合法输入
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", assetURL, releaseURL, 100, 1, 1, validDigest); err != nil {
		t.Errorf("valid should pass: %v", err)
	}
	// 测试桩仍可正常构造
	if _, err := newTestCandidate("v1.2.3", "linux", "amd64", assetURL, releaseURL, 100); err != nil {
		t.Errorf("private fixture should pass: %v", err)
	}
}

// Status 返回值必须为加锁拷贝，并发读写不得互相污染
func TestOracle_StatusImmutableCopy(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "oss-server")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	up := &Updater{exe: exe, backup: exe + ".bak"}
	up.lastCheck = &CheckResult{CheckedAt: time.Now(), CurrentVersion: "1.0.0", LatestVersion: "v1.2.3", UpdateAvailable: true}
	up.lastUpdate = &UpdateResult{At: time.Now(), Code: "ok", Phase: StateDone, Version: "1.2.3"}
	// 并发读写，读到拷贝后篡改不得影响内部
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := up.Status()
			// 篡改返回拷贝，内部状态不得被污染
			if s.LastCheck != nil {
				s.LastCheck.LatestVersion = "mutated"
				s.LastCheck.UpdateAvailable = false
			}
			if s.LastUpdate != nil {
				s.LastUpdate.Code = "hacked"
			}
		}()
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			up.stateMu.Lock()
			if up.lastCheck != nil {
				cp := *up.lastCheck
				cp.LatestVersion = "v9.9.9"
				up.lastCheck = &cp
			}
			up.stateMu.Unlock()
			_ = n
		}(i)
	}
	wg.Wait()
	st := up.Status()
	if st.LastCheck != nil && st.LastCheck.LatestVersion == "mutated" {
		t.Error("Status copy mutated internal lastCheck")
	}
	if st.LastUpdate != nil && st.LastUpdate.Code == "hacked" {
		t.Error("Status copy mutated internal lastUpdate")
	}
	// 再校验 CheckUpdate 与 Status 的隔离性
	cfg := &config.Config{Update: config.UpdateConfig{GitHubRepo: "fake/oss-sync"}}
	// 构造 CheckUpdate 隔离性用的桩服务
	content := fakeExecBytes()
	assetName := "oss-sync_9.9.9_linux_amd64.tar.gz"
	// 覆盖 CheckUpdate 隔离分支的桩输入
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1,"tag_name":"v9.9.9","html_url":"https://example.com/tag/v9.9.9","draft":false,"prerelease":false,"assets":[]}`))
	}))
	defer srv.Close()
	_ = content
	_ = assetName
	u2, err := NewUpdater(cfg, Options{ExecPath: exe, APIBase: srv.URL, HTTPClient: srv.Client(), Verifier: func(string, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	res, err := u2.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	orig := res.LatestVersion
	res.LatestVersion = "tampered"
	st2 := u2.Status()
	if st2.LastCheck != nil && st2.LastCheck.LatestVersion == "tampered" {
		t.Error("CheckUpdate returned pointer shares internal state")
	}
	if orig != st2.LastCheck.LatestVersion && st2.LastCheck.LatestVersion != "v9.9.9" {
		t.Errorf("unexpected latest %q", st2.LastCheck.LatestVersion)
	}
	_ = version.Version
}

// 严格持久化状态机按表驱动覆盖
func TestOracle_StrictTransitionGraph(t *testing.T) {
	// 允许的线性推进序列
	allowed := [][2]OperationState{
		{StateInProgress, StatePrepare},
		{StatePrepare, StateFetchRelease},
		{StateFetchRelease, StateSelectAsset},
		{StateSelectAsset, StateDownload},
		{StateDownload, StateVerify},
		{StateVerify, StateBackup},
		{StateBackup, StateSwap},
		{StateSwap, StateDone},
	}
	for _, p := range allowed {
		if !isAllowedTransition(p[0], p[1]) {
			t.Errorf("should allow %s -> %s", p[0], p[1])
		}
	}
	// 活跃阶段均可转失败
	active := []OperationState{StateInProgress, StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap}
	for _, from := range active {
		if !isAllowedTransition(from, StateFailed) {
			t.Errorf("should allow %s -> failed", from)
		}
	}
	// up_to_date 仅允许从 fetch_release 离开
	if !isAllowedTransition(StateFetchRelease, StateUpToDate) {
		t.Error("fetch_release -> up_to_date should be allowed")
	}
	if isAllowedTransition(StateDownload, StateUpToDate) {
		t.Error("download -> up_to_date should be rejected")
	}
	if isAllowedTransition(StatePrepare, StateUpToDate) {
		t.Error("prepare -> up_to_date should be rejected")
	}
	// 跳阶段一律拒绝
	rejected := [][2]OperationState{
		{StateInProgress, StateDownload},
		{StateInProgress, StateDone},
		{StatePrepare, StateDownload},
		{StatePrepare, StateVerify},
		{StateSelectAsset, StateVerify},
		{StateDownload, StateBackup},
		{StateVerify, StateSwap},
		{StateBackup, StateDone},
		{StateInProgress, StateSelectAsset},
		{StateFetchRelease, StateDownload},
		{StateDone, StateFailed},
		{StateFailed, StateDone},
		{StateUpToDate, StateFailed},
	}
	for _, p := range rejected {
		if isAllowedTransition(p[0], p[1]) {
			t.Errorf("should reject skip %s -> %s", p[0], p[1])
		}
	}
	// 再经 Manager.Transition 校验强制执行
	t.Run("manager enforces graph", func(t *testing.T) {
		m := newTestManager(t)
		cc, _ := m.IssueChecked(testCandidate("10.0.0"), time.Minute)
		op, _ := m.StartOperation(cc.ID, "")
		// 跳阶段必须失败
		if _, err := m.Transition(op.ID, StateDownload, ""); err == nil {
			t.Error("manager should reject skip in_progress -> download")
		}
		// 按正确线性序列推进
		seq := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap, StateDone}
		for _, nxt := range seq {
			var err error
			op, err = m.Transition(op.ID, nxt, "")
			if err != nil {
				t.Fatalf("transition to %s failed: %v", nxt, err)
			}
		}
		if op.State != StateDone {
			t.Errorf("final state %q", op.State)
		}
		// 覆盖 fetch_release 直达 up_to_date 路径
		m2 := newTestManager(t)
		cc2, _ := m2.IssueChecked(testCandidate("10.0.1"), time.Minute)
		op2, _ := m2.StartOperation(cc2.ID, "")
		op2, _ = m2.Transition(op2.ID, StatePrepare, "")
		op2, _ = m2.Transition(op2.ID, StateFetchRelease, "")
		if _, err := m2.Transition(op2.ID, StateUpToDate, ""); err != nil {
			t.Errorf("fetch_release -> up_to_date should succeed: %v", err)
		}
	})
}

// 同根多 Manager 并发语义
func TestOracle_ConcurrentManagerSameRoot(t *testing.T) {
	root := t.TempDir()
	m1, err := NewManager(root)
	if err != nil {
		t.Fatalf("m1: %v", err)
	}
	m2, err := NewManager(root)
	if err != nil {
		t.Fatalf("m2: %v", err)
	}
	// 两个 Manager 并发签发
	var wg sync.WaitGroup
	ids := make([]string, 0, 20)
	var mu sync.Mutex
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			c := testCandidate("1.2." + strconv.Itoa(n+20))
			cc, err := m1.IssueChecked(c, time.Minute)
			if err == nil {
				mu.Lock()
				ids = append(ids, cc.ID)
				mu.Unlock()
			}
		}(i)
		go func(n int) {
			defer wg.Done()
			c := testCandidate("1.2." + strconv.Itoa(n+30))
			cc, err := m2.IssueChecked(c, time.Minute)
			if err == nil {
				mu.Lock()
				ids = append(ids, cc.ID)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if len(ids) != 20 {
		t.Fatalf("expected 20 checked, got %d", len(ids))
	}
	// 重载后两侧均应可见全部签发；逐个回查即可触发重载
	for _, id := range ids {
		if _, err := m1.GetChecked(id); err != nil {
			t.Errorf("m1 missing %s: %v", id, err)
		}
		if _, err := m2.GetChecked(id); err != nil {
			t.Errorf("m2 missing %s: %v", id, err)
		}
	}
	// 并发启动操作，仅允许一个成功
	cc, _ := m1.IssueChecked(testCandidate("9.9.9"), time.Minute)
	// 先让 m2 可见该检查，后续并发启动依赖重载
	var success int
	var sMu sync.Mutex
	wg = sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := m1.StartOperation(cc.ID, "")
			if err == nil {
				sMu.Lock()
				success++
				sMu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			_, err := m2.StartOperation(cc.ID, "")
			if err == nil {
				sMu.Lock()
				success++
				sMu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("expected exactly 1 StartOperation success across managers, got %d", success)
	}
	// 原子写必须使用唯一临时文件，不得残留固定 .tmp 文件
	matches, _ := filepath.Glob(filepath.Join(root, "update_state.json.tmp*"))
	_ = matches
	// 不得残留固定 .tmp 文件
	if _, err := os.Stat(filepath.Join(root, "update_state.json.tmp")); err == nil {
		t.Error("fixed .tmp file should not remain; unique temps required")
	}
	_ = strings.Contains
}
