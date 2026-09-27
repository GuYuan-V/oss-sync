package update

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/helantianshen/oss-sync/internal/version"
)

// CheckCapability 校验自更新的平台、版本与可执行文件条件
// 不满足时返回带稳定 Code 的 UpdateError
func CheckCapability(execPath string, goos, goarch string) error {
	if version.IsDevelopmentVersion(version.Version) {
		return newUpdateError(
			CodeDevelopmentVersion,
			fmt.Sprintf("development version %q is not eligible for self-update", version.Version),
			ErrDevelopmentVersion,
		)
	}
	if !IsSupportedPlatform(goos, goarch) {
		return newUpdateError(
			CodeUnsupportedPlatform,
			fmt.Sprintf("unsupported platform %s/%s", goos, goarch),
			ErrUnsupportedPlatform,
		)
	}
	if execPath == "" {
		return newUpdateError(CodeNotRegularFile, "executable path is empty", ErrNotRegularFile)
	}
	info, err := os.Lstat(execPath)
	if err != nil {
		return newUpdateError(CodeNotRegularFile, fmt.Sprintf("cannot stat executable %q: %v", execPath, err), err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return newUpdateError(CodeSymlinkNotAllowed, fmt.Sprintf("executable %q is a symlink", execPath), ErrSymlinkNotAllowed)
	}
	if !info.Mode().IsRegular() {
		return newUpdateError(CodeNotRegularFile, fmt.Sprintf("executable %q is not a regular file", execPath), ErrNotRegularFile)
	}
	dir := filepath.Dir(execPath)
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return newUpdateError(CodeUnwritableDirectory, fmt.Sprintf("cannot stat executable directory %q: %v", dir, err), err)
	}
	if !dirInfo.IsDir() {
		return newUpdateError(CodeUnwritableDirectory, fmt.Sprintf("executable directory %q is not a directory", dir), nil)
	}
	// 目录可写性：尝试创建并立即删除临时文件，避免仅依赖权限位的误判
	tmpFile, err := os.CreateTemp(dir, ".oss-write-check-*")
	if err != nil {
		return newUpdateError(CodeUnwritableDirectory, fmt.Sprintf("executable directory %q is not writable: %v", dir, err), err)
	}
	tmpName := tmpFile.Name()
	_ = tmpFile.Close()
	_ = os.Remove(tmpName)
	return nil
}

// ManagedUpdateEnabled 判断当前为 systemd 部署且已安装网页触发更新的 path unit
// 此时更新由宿主机 oss-sync-update.path/.service 以 root 托管，网页只写请求文件
func ManagedUpdateEnabled() bool {
	return os.Getenv("OSS_UPDATE_MANAGER") == "systemd" && os.Getenv("OSS_UPDATE_VIA_PATH_UNIT") == "1"
}

// CheckCurrentCapability 使用当前版本与平台校验能力
func CheckCurrentCapability(execPath string) error {
	// systemd 负责进程生命周期，进程内 helper 交接不能接管服务
	if os.Getenv("OSS_UPDATE_MANAGER") == "systemd" {
		// 安装了 path unit 时改由宿主机托管更新，网页写请求文件触发
		if ManagedUpdateEnabled() {
			return ErrManagedUpdate
		}
		return ErrExternalUpdate
	}
	return CheckCapability(execPath, runtime.GOOS, runtime.GOARCH)
}
