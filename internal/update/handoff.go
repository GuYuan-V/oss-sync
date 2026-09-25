package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/helantianshen/oss-sync/internal/version"
)

// HelperFlag 为隐藏的 helper 启动标记，可跳过常规配置与数据库初始化
const HelperFlag = "--oss-update-helper"

const helperMarkerExt = ".handoff.json"

// HandoffMarker 为 helper 收到的持久化交接引用
// 其中不含令牌，仅保存文件路径与操作 ID
type HandoffMarker struct {
	OpID          string   `json:"op_id"`
	ManagerRoot   string   `json:"manager_root"`
	ExecPath      string   `json:"exec_path"`
	StagedPath    string   `json:"staged_path"`
	BackupPath    string   `json:"backup_path"`
	HelperPath    string   `json:"helper_path"`
	TargetVersion string   `json:"target_version"`
	Digest        string   `json:"digest"` // 暂存可执行文件的 SHA-256，而非发布压缩包
	ParentPID     int      `json:"parent_pid"`
	ReadyURL      string   `json:"ready_url"`
	OrigArgs      []string `json:"orig_args"`
	WorkDir       string   `json:"work_dir"`
}

// IsHelperInvocation 判断当前进程是否为 helper 调用
func IsHelperInvocation() (bool, string) {
	args := os.Args[1:]
	for i, a := range args {
		if a == HelperFlag {
			if i+1 < len(args) {
				return true, args[i+1]
			}
			return true, ""
		}
		if strings.HasPrefix(a, HelperFlag+"=") {
			return true, strings.TrimPrefix(a, HelperFlag+"=")
		}
	}
	return false, ""
}

// helperMarkerDir 返回与可执行文件同文件系统的暂存目录
func helperMarkerDir(execPath string) string {
	return filepath.Join(filepath.Dir(execPath), ".oss-update-pending")
}

// helperMarkerPath 返回指定操作的标记文件路径
func helperMarkerPath(execPath, opID string) string {
	dir := helperMarkerDir(execPath)
	return filepath.Join(dir, opID+helperMarkerExt)
}

// atomicWriteMarker 错误传播测试用的注入点
var openFileForSyncFn = func(name string) (*os.File, error) { return os.OpenFile(name, os.O_RDWR, 0) }
var syncFileFn = func(f *os.File) error { return f.Sync() }
var openDirFn = func(name string) (*os.File, error) { return os.Open(name) }
var syncDirFn = func(f *os.File) error { return f.Sync() }
var removeFileFn = os.Remove

// atomicWriteMarker 把标记序列化为 JSON，经 fsync 与改名原子写入
// 临时文件与目录的同步或打开错误一律向上返回；仅在操作系统明确报告不支持目录同步时
// （例如 Windows 上的 syscall.EINVAL）保留一条窄例外；其余错误均返回并清理临时文件
func atomicWriteMarker(markerPath string, marker HandoffMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	dir := filepath.Dir(markerPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := markerPath + ".tmp." + uuid.NewString()
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	// 同步临时文件，错误直接返回
	if f, err := openFileForSyncFn(tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("open temp for sync: %w", err)
	} else {
		if err := syncFileFn(f); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("fsync temp: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("close temp: %w", err)
		}
	}
	if err := os.Rename(tmp, markerPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// 同步目录，打开或同步错误直接返回，仅保留 Windows 不支持目录同步的窄例外
	df, err := openDirFn(dir)
	if err != nil {
		return fmt.Errorf("open dir for sync: %w", err)
	}
	if err := syncDirFn(df); err != nil {
		_ = df.Close()
		// 仅限 Windows 的窄例外：目录同步可能不受支持
		// Windows 上可能返回 EINVAL 或 ENOSYS，此处视为非致命错误
		if runtime.GOOS == "windows" && isWindowsDirSyncUnsupported(err) {
			return nil
		}
		return fmt.Errorf("fsync dir: %w", err)
	}
	if err := df.Close(); err != nil {
		return fmt.Errorf("close dir: %w", err)
	}
	return nil
}

func isWindowsDirSyncUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Windows 目录 fsync 常不受支持；在 Windows 上打开目录同步常报拒绝访问
	// 窄例外的判定条件为仅限 Windows，且仅匹配已知的不支持提示
	if runtime.GOOS != "windows" {
		return false
	}
	return strings.Contains(msg, "invalid") || strings.Contains(msg, "not supported") || strings.Contains(msg, "enotsup") || strings.Contains(msg, "einval") || strings.Contains(msg, "access is denied") || strings.Contains(msg, "denied")
}

// isSafePath 确认 p 位于 base 目录内，且不存在路径穿越
func isSafePath(base, p string) bool {
	if p == "" {
		return false
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	pAbs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, pAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func validateMarkerSafe(m *HandoffMarker) error {
	if m == nil {
		return errors.New("marker is nil")
	}
	if !isValidDigest(m.Digest) {
		return fmt.Errorf("invalid digest %q", m.Digest)
	}
	normWant := version.Normalize(m.TargetVersion)
	if normWant == "" || !version.IsValid(normWant) {
		return fmt.Errorf("invalid target version %q", m.TargetVersion)
	}
	if m.TargetVersion != normWant {
		return fmt.Errorf("target version %q must be normalized", m.TargetVersion)
	}
	base := helperMarkerDir(m.ExecPath)
	if !isSafePath(base, m.StagedPath) || !isSafePath(base, m.BackupPath) || !isSafePath(base, m.HelperPath) {
		return fmt.Errorf("unsafe marker paths")
	}
	if m.ExecPath == "" || !filepath.IsAbs(m.ExecPath) {
		return fmt.Errorf("unsafe exec path")
	}
	return nil
}

// ResumePendingHandoffs 在常规启动时发现持久化的待处理标记，并在校验操作有效性与标记安全性后恢复 helper
// 其覆盖的场景为标记已写入但 helper 尚未启动的崩溃，此时直接启动 helper 继续执行
// 损坏或非活跃的标记一律不处理，既不回滚也不删除
func ResumePendingHandoffs(execPath string) (int, error) {
	dir := helperMarkerDir(execPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	resumed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), helperMarkerExt) {
			continue
		}
		markerPath := filepath.Join(dir, e.Name())
		// 行动前先校验操作有效性与标记安全性
		_, _, err := recoverActiveMarker(markerPath)
		if err != nil {
			// 损坏或非活跃标记一律不处理
			continue
		}
		// 读入标记，校验路径安全性、digest 与目标版本
		data, err := os.ReadFile(markerPath)
		if err != nil {
			continue
		}
		var m HandoffMarker
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if err := validateMarkerSafe(&m); err != nil {
			continue
		}
		// 统一由 helper 收尾：启动 helper 完成等待、替换、探测与回滚
		if err := launchHelperFn(m.ExecPath, markerPath); err != nil {
			// 普通启动阶段无法拉起 helper 时（如进程创建失败），主进程直接回滚
			_ = recordRollback(&m, fmt.Sprintf("resume launch failed: %v", err))
			continue
		}
		resumed++
	}
	return resumed, nil
}

// CheckHandoffCapability 校验 helper 自更新的平台与文件条件
func CheckHandoffCapability(execPath string) error {
	// 仅支持 linux、darwin 与 windows，其余 GOOS 视为不支持
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		return newUpdateError(CodeUnsupportedPlatform, fmt.Sprintf("helper not supported on %s", runtime.GOOS), ErrUnsupportedPlatform)
	}
	return CheckCurrentCapability(execPath)
}

// verifyStagedFile 校验 digest、可执行文件魔数与精确的 --version 输出，全程无需联网
// 调用方须保证 stagedPath 与目标可执行文件位于同一文件系统
func verifyStagedFile(stagedPath, digest, wantVersion string) error {
	if stagedPath == "" {
		return errors.New("staged path is empty")
	}
	if _, err := os.Stat(stagedPath); err != nil {
		return fmt.Errorf("staged file missing: %w", err)
	}
	if digest != "" {
		if err := verifyFileDigest(stagedPath, digest); err != nil {
			return fmt.Errorf("staged digest mismatch: %w", err)
		}
	}
	if err := checkExecutableMagic(stagedPath, runtime.GOOS); err != nil {
		return fmt.Errorf("staged magic check failed: %w", err)
	}
	// 版本必须规范化后精确相等，禁止子串匹配（如 1.2.3 与 1.2.30 不得混淆）
	if wantVersion != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, stagedPath, "--version")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("staged --version failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		got := strings.TrimSpace(string(out))
		if got == "" {
			return errors.New("staged --version output empty")
		}
		if version.Normalize(got) != version.Normalize(wantVersion) {
			return fmt.Errorf("staged version %q (normalized %q) does not equal target %q (normalized %q)", got, version.Normalize(got), wantVersion, version.Normalize(wantVersion))
		}
	}
	return nil
}

var verifyStagedFileFn = verifyStagedFile

// prepareStaging 把候选二进制与当前可执行文件副本复制到可执行文件所在文件系统，随后校验暂存文件
func prepareStaging(candidatePath, execPath, opID, wantVersion, digest string) (staged, backup, helperCopy string, err error) {
	if err := CheckHandoffCapability(execPath); err != nil {
		return "", "", "", err
	}
	dir := helperMarkerDir(execPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create staging dir: %w", err)
	}
	staged = filepath.Join(dir, "staged-"+opID)
	backup = filepath.Join(dir, "backup-"+opID)
	helperCopy = filepath.Join(dir, "helper-"+opID)

	// 把候选文件复制到暂存位置
	if err := copyFile(candidatePath, staged); err != nil {
		return "", "", "", fmt.Errorf("stage candidate: %w", err)
	}
	// 保证暂存文件在 Unix 上可执行
	_ = os.Chmod(staged, 0o755)
	if err := verifyStagedFileFn(staged, digest, wantVersion); err != nil {
		_ = os.Remove(staged)
		return "", "", "", err
	}
	// 把当前二进制复制为备份
	if err := copyFile(execPath, backup); err != nil {
		_ = os.Remove(staged)
		return "", "", "", fmt.Errorf("backup current: %w", err)
	}
	// 复制 helper 副本（与当前二进制相同），提高恢复能力
	if err := copyFile(execPath, helperCopy); err != nil {
		_ = os.Remove(staged)
		_ = os.Remove(backup)
		return "", "", "", fmt.Errorf("stage helper copy: %w", err)
	}
	return staged, backup, helperCopy, nil
}

// launchHelper 以分离方式启动 helper 进程，helper 仅接收标记路径
var launchHelperFn = launchHelper

func launchHelper(execPath, markerPath string) error {
	if markerPath == "" {
		return errors.New("marker path empty")
	}
	// 优先使用同文件系统的 helper 副本，不存在时回退到当前可执行文件
	helperBin := execPath
	// 读取标记，确认 HelperPath 是否存在
	if data, err := os.ReadFile(markerPath); err == nil {
		var m HandoffMarker
		if json.Unmarshal(data, &m) == nil && m.HelperPath != "" {
			if _, err := os.Stat(m.HelperPath); err == nil {
				helperBin = m.HelperPath
			}
		}
	}
	cmd := exec.Command(helperBin, HelperFlag+"="+markerPath)
	cmd.Env = os.Environ()
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	detachHelper(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch helper: %w", err)
	}
	// 不等待 helper，helper 独立运行
	_ = cmd.Process.Release()
	return nil
}

// waitForParent 轮询至父进程退出或超时
var waitForParentFn = waitForParent

func waitForParent(parentPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(parentPID) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if isProcessAlive(parentPID) {
		return fmt.Errorf("parent %d still alive after %s", parentPID, timeout)
	}
	return nil
}

// atomicReplace 执行暂存文件到可执行文件路径的替换
var atomicReplaceFn = atomicReplace

func atomicReplace(stagedPath, execPath string) error {
	return swapBinary(stagedPath, execPath)
}

// recoverActiveMarker 校验标记是否引用仍活跃的持久化操作
// 有效时返回标记与操作；已终态或非活跃时返回错误
// 标记缺失或操作非活跃表示这是一次常规启动，不回滚
func recoverActiveMarker(markerPath string) (*HandoffMarker, *Operation, error) {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read marker: %w", err)
	}
	var m HandoffMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil, fmt.Errorf("corrupted marker: %w", err)
	}
	if m.OpID == "" || m.ManagerRoot == "" {
		return nil, nil, errors.New("marker missing op or root")
	}
	mgr, err := NewManager(m.ManagerRoot)
	if err != nil && !errors.Is(err, ErrCorruptedState) {
		return nil, nil, fmt.Errorf("open manager: %w", err)
	}
	// 状态损坏时 NewManager 仍返回可用实例，直接用于查询
	if mgr == nil {
		return nil, nil, errors.New("manager unavailable")
	}
	op, err := mgr.GetOperation(m.OpID)
	if err != nil {
		return nil, nil, fmt.Errorf("operation not found: %w", err)
	}
	if op.IsTerminal() {
		return nil, nil, fmt.Errorf("operation already terminal %s", op.State)
	}
	// 同时确认该操作仍为当前活跃操作
	active := mgr.ActiveOperation()
	if active == nil || active.ID != m.OpID {
		return nil, nil, errors.New("operation not active")
	}
	return &m, op, nil
}

// InitiateHelperHandoff 暂存候选文件并启动 helper
// 交接前校验 digest、魔数与 --version，成功返回活跃的 Operation
// mgr 为持久化 Manager，checkID 标识候选，candidatePath 为候选二进制路径
// binaryDigest 是二进制摘要；readyURL、origArgs 和 workDir 用于启动与就绪检查
func (u *Updater) InitiateHelperHandoff(mgr *Manager, checkID string, candidatePath string, binaryDigest string, readyURL string, origArgs []string, workDir string) (*Operation, error) {
	if mgr == nil {
		return nil, errors.New("manager is nil")
	}
	if err := CheckHandoffCapability(u.exe); err != nil {
		return nil, err
	}
	if candidatePath == "" {
		return nil, errors.New("candidatePath is empty")
	}
	if !isValidDigest(binaryDigest) {
		return nil, newUpdateError(CodeInvalidAsset, "prepared executable digest missing or malformed", ErrInvalidAsset)
	}
	cand, err := mgr.ValidateChecked(checkID)
	if err != nil {
		return nil, err
	}
	if err := cand.Validate(); err != nil {
		return nil, err
	}
	// 先创建持久化操作，其为唯一的事实来源
	op, err := mgr.StartOperation(checkID, cand.Version)
	if err != nil {
		return nil, err
	}
	// 交接前把状态推进到备份阶段，替换与完成由 helper 收尾
	seq := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup}
	for _, nxt := range seq {
		cur, _ := mgr.GetOperation(op.ID)
		if cur.State == nxt {
			continue
		}
		if isAllowedTransition(cur.State, nxt) {
			if _, err := mgr.Transition(op.ID, nxt, ""); err != nil {
				// 状态推进失败时置为失败并中止
				_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
				return nil, err
			}
		}
	}
	// 在可执行文件所在文件系统暂存文件，交接前完成校验
	staged, backup, helperCopy, err := prepareStaging(candidatePath, u.exe, op.ID, cand.Version, binaryDigest)
	if err != nil {
		_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
		return nil, err
	}
	// 备份路径仅用于内部诊断，不对外暴露；Manager 未提供经 Transition 修改该字段的接口，此处保留局部变量备查
	_ = backup
	_ = helperCopy
	// 创建交接标记
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	if len(origArgs) == 0 {
		origArgs = os.Args
	}
	marker := HandoffMarker{
		OpID:          op.ID,
		ManagerRoot:   mgr.root,
		ExecPath:      u.exe,
		StagedPath:    staged,
		BackupPath:    backup,
		HelperPath:    helperCopy,
		TargetVersion: cand.Version,
		Digest:        binaryDigest,
		ParentPID:     os.Getpid(),
		ReadyURL:      readyURL,
		OrigArgs:      origArgs,
		WorkDir:       workDir,
	}
	markerPath := helperMarkerPath(u.exe, op.ID)
	dir := helperMarkerDir(u.exe)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
		return nil, err
	}
	if err := atomicWriteMarker(markerPath, marker); err != nil {
		// 标记可能已持久化（改名后的目录同步或打开失败），需按是否可恢复分别处理
		if _, statErr := os.Stat(markerPath); statErr == nil {
			// 改名后标记仍存在，可能已持久化；先尝试删除以验证清理是否成功
			rmErr := removeFileFn(markerPath)
			_, statAfter := os.Stat(markerPath)
			stillDurable := statAfter == nil
			if stillDurable {
				// 标记仍存在，清理未被证实，保持为可恢复的已提交交接，不置为失败
				if cur, _ := mgr.GetOperation(op.ID); cur != nil && isAllowedTransition(cur.State, StateSwap) {
					_, _ = mgr.Transition(op.ID, StateSwap, "")
				}
				if fresh, gErr := mgr.GetOperation(op.ID); gErr == nil {
					return fresh, nil
				}
				return op, nil
			}
			// 标记已删除，再持久化终态；两者都成功才算清理完成
			transOp, transErr := mgr.Transition(op.ID, StateFailed, err.Error())
			_ = transOp
			if transErr == nil && rmErr == nil {
				_ = removeFileFn(staged)
				_ = removeFileFn(backup)
				_ = removeFileFn(helperCopy)
				return nil, fmt.Errorf("write marker: %w", err)
			}
			// 标记已删除但终态持久化失败，此时无可恢复标记，按失败返回不产生矛盾
			_ = removeFileFn(staged)
			_ = removeFileFn(backup)
			_ = removeFileFn(helperCopy)
			if transErr != nil {
				return nil, fmt.Errorf("write marker: %w (terminal persist failed: %v)", err, transErr)
			}
			return nil, fmt.Errorf("write marker: %w", err)
		}
		// 标记确定未持久化（改名前失败），持久化终态并清理暂存文件后返回
		_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
		_ = removeFileFn(staged)
		_ = removeFileFn(backup)
		_ = removeFileFn(helperCopy)
		_ = removeFileFn(markerPath)
		return nil, fmt.Errorf("write marker: %w", err)
	}
	// 进入替换阶段，表示交接进行中
	if cur, _ := mgr.GetOperation(op.ID); isAllowedTransition(cur.State, StateSwap) {
		_, _ = mgr.Transition(op.ID, StateSwap, "")
	}
	if err := launchHelperFn(u.exe, markerPath); err != nil {
		// 先删除标记再持久化失败态，两者都被证实时才按失败返回
		rmErr := removeFileFn(markerPath)
		_, statAfter := os.Stat(markerPath)
		stillDurable := statAfter == nil
		if stillDurable {
			// 标记仍存在，保持为可恢复的已提交交接，不置为失败
			if fresh, gErr := mgr.GetOperation(op.ID); gErr == nil {
				return fresh, nil
			}
			return op, nil
		}
		transOp, transErr := mgr.Transition(op.ID, StateFailed, err.Error())
		_ = transOp
		if transErr == nil && rmErr == nil {
			_ = removeFileFn(helperCopy)
			return nil, fmt.Errorf("helper launch failure: %w", err)
		}
		// 标记已删除但终态持久化失败，此时无可恢复标记，按失败返回
		_ = removeFileFn(helperCopy)
		if transErr != nil {
			return nil, fmt.Errorf("helper launch failure: %w (terminal persist failed: %v)", err, transErr)
		}
		return nil, fmt.Errorf("helper launch failure: %w", err)
	}
	if fresh, gErr := mgr.GetOperation(op.ID); gErr == nil {
		return fresh, nil
	}
	return op, nil
}

// recordSuccess 把操作置为完成并清理标记
func recordSuccess(m *HandoffMarker) error {
	mgr, err := NewManager(m.ManagerRoot)
	if err != nil && !errors.Is(err, ErrCorruptedState) {
		return err
	}
	// 状态图要求经 Swap 到达 Done，状态靠前时逐步推进
	op, err := mgr.GetOperation(m.OpID)
	if err != nil {
		return err
	}
	// 沿线性状态链尽力推进到 Done
	chain := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap, StateDone}
	for _, want := range chain {
		cur, _ := mgr.GetOperation(m.OpID)
		if cur.State == want {
			continue
		}
		if cur.State == StateDone || cur.IsTerminal() {
			break
		}
		if isAllowedTransition(cur.State, want) {
			if _, err := mgr.Transition(m.OpID, want, ""); err != nil {
				// 非直接后继时继续寻找下一个允许的迁移
				continue
			}
		}
		_ = op
	}
	// 尚未终态时保证最终到达 Done
	cur, _ := mgr.GetOperation(m.OpID)
	if !cur.IsTerminal() {
		if isAllowedTransition(cur.State, StateDone) {
			_, _ = mgr.Transition(m.OpID, StateDone, "")
		} else if isAllowedTransition(cur.State, StateSwap) {
			_, _ = mgr.Transition(m.OpID, StateSwap, "")
			_, _ = mgr.Transition(m.OpID, StateDone, "")
		} else {
			// 不允许经失败态到达完成态，此处再次沿状态链顺序推进
			for _, want := range chain {
				c, _ := mgr.GetOperation(m.OpID)
				if c.IsTerminal() {
					break
				}
				if isAllowedTransition(c.State, want) {
					_, _ = mgr.Transition(m.OpID, want, "")
				}
			}
		}
	}
	_ = os.Remove(m.StagedPath)
	_ = os.Remove(m.HelperPath)
	// 成功后保留备份，删除标记即表示交接完成
	_ = os.Remove(markerPathFor(m))
	return nil
}

// recordRollback 恢复旧二进制、拉起旧服务，并把操作置为失败或已回滚
func recordRollback(m *HandoffMarker, cause string) error {
	mgr, err := NewManager(m.ManagerRoot)
	if err != nil && !errors.Is(err, ErrCorruptedState) {
		return err
	}
	// 仅在标记仍有效时恢复备份
	if _, err := os.Stat(m.BackupPath); err == nil {
		_ = swapBinary(m.BackupPath, m.ExecPath)
	}
	// 按原始参数拉起旧服务
	relaunchOldServerFn(m)
	if mgr != nil {
		cur, _ := mgr.GetOperation(m.OpID)
		if cur != nil && !cur.IsTerminal() {
			// 允许时直接进入失败态
			if isAllowedTransition(cur.State, StateFailed) {
				_, _ = mgr.Transition(m.OpID, StateFailed, cause)
			} else {
				// 沿状态链推进到允许进入失败态的位置
				chain := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap}
				for _, want := range chain {
					c, _ := mgr.GetOperation(m.OpID)
					if c.IsTerminal() {
						break
					}
					if isAllowedTransition(c.State, want) {
						_, _ = mgr.Transition(m.OpID, want, "")
					}
					c2, _ := mgr.GetOperation(m.OpID)
					if isAllowedTransition(c2.State, StateFailed) {
						_, _ = mgr.Transition(m.OpID, StateFailed, cause)
						break
					}
				}
			}
		}
	}
	_ = os.Remove(m.StagedPath)
	_ = os.Remove(m.HelperPath)
	_ = os.Remove(markerPathFor(m))
	return nil
}

func markerPathFor(m *HandoffMarker) string {
	return helperMarkerPath(m.ExecPath, m.OpID)
}

var relaunchOldServerFn = relaunchOldServer

// 跨包测试用的注入点
func SetLaunchHelperFn(fn func(string, string) error) {
	if fn == nil {
		launchHelperFn = launchHelper
	} else {
		launchHelperFn = fn
	}
}

// SetVerifyStagedFileFn 仅供测试替换候选文件校验器；nil 恢复默认实现
func SetVerifyStagedFileFn(fn func(string, string, string) error) {
	if fn == nil {
		verifyStagedFileFn = verifyStagedFile
	} else {
		verifyStagedFileFn = fn
	}
}

// SetWaitForParentFn 仅供测试替换父进程等待函数；nil 恢复默认实现
func SetWaitForParentFn(fn func(int, time.Duration) error) {
	if fn == nil {
		waitForParentFn = waitForParent
	} else {
		waitForParentFn = fn
	}
}

// SetAtomicReplaceFn 仅供测试替换原子替换函数；nil 恢复默认实现
func SetAtomicReplaceFn(fn func(string, string) error) {
	if fn == nil {
		atomicReplaceFn = atomicReplace
	} else {
		atomicReplaceFn = fn
	}
}

// SetOpenFileForSyncFn 仅供测试替换文件打开函数；nil 恢复默认实现
func SetOpenFileForSyncFn(fn func(string) (*os.File, error)) {
	if fn == nil {
		openFileForSyncFn = func(name string) (*os.File, error) { return os.OpenFile(name, os.O_RDWR, 0) }
	} else {
		openFileForSyncFn = fn
	}
}

// SetSyncFileFn 仅供测试替换文件同步函数；nil 恢复默认实现
func SetSyncFileFn(fn func(*os.File) error) {
	if fn == nil {
		syncFileFn = func(f *os.File) error { return f.Sync() }
	} else {
		syncFileFn = fn
	}
}

// SetOpenDirFn 仅供测试替换目录打开函数；nil 恢复默认实现
func SetOpenDirFn(fn func(string) (*os.File, error)) {
	if fn == nil {
		openDirFn = func(name string) (*os.File, error) { return os.Open(name) }
	} else {
		openDirFn = fn
	}
}

// SetSyncDirFn 仅供测试替换目录同步函数；nil 恢复默认实现
func SetSyncDirFn(fn func(*os.File) error) {
	if fn == nil {
		syncDirFn = func(f *os.File) error { return f.Sync() }
	} else {
		syncDirFn = fn
	}
}

// SetRelaunchOldServerFn 仅供测试替换回滚重启函数；nil 恢复默认实现
func SetRelaunchOldServerFn(fn func(*HandoffMarker)) {
	if fn == nil {
		relaunchOldServerFn = relaunchOldServer
	} else {
		relaunchOldServerFn = fn
	}
}

// SetRemoveFileFn 仅供测试替换删除函数；nil 恢复默认实现
func SetRemoveFileFn(fn func(string) error) {
	if fn == nil {
		removeFileFn = os.Remove
	} else {
		removeFileFn = fn
	}
}

// relaunchOldServer 按原始上下文启动恢复后的可执行文件
func relaunchOldServer(m *HandoffMarker) {
	if m.ExecPath == "" {
		return
	}
	args := m.OrigArgs
	if len(args) == 0 {
		args = []string{m.ExecPath}
	}
	// 首个参数固定为可执行文件路径
	cmd := exec.Command(m.ExecPath, args[1:]...)
	cmd.Env = os.Environ()
	if m.WorkDir != "" {
		cmd.Dir = m.WorkDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	detachHelper(cmd)
	_ = cmd.Start()
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}
