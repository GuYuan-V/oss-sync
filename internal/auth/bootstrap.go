package auth

import (
<<<<<<< HEAD
	"fmt"
	"os"

=======
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

<<<<<<< HEAD
// EnsureBootstrapAdmin 通过 OSS_ADMIN_PASSWORD 预置管理员。
//
// 新数据库不再在终端交互询问管理员密码：若设置了 OSS_ADMIN_PASSWORD
// 则直接创建预置管理员；否则不做任何事，第一个网页注册账户自动成为管理员。
func EnsureBootstrapAdmin(db *gorm.DB, cfg *config.Config) (bool, error) {
=======
// EnsureBootstrapAdmin 保证服务启动前至少存在一个管理员。
//
// 首次启动优先读取 OSS_ADMIN_PASSWORD；未设置时只在真实交互终端中隐藏输入。
// 非交互式部署必须显式提供环境变量，避免固定默认密码和服务静默暴露。
func EnsureBootstrapAdmin(db *gorm.DB, cfg *config.Config, input *os.File, output io.Writer) (bool, error) {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	var count int64
	if err := db.Model(&models.User{}).Where("role = ?", "admin").Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查管理员账户失败: %w", err)
	}
	if count > 0 {
		return false, nil
	}

<<<<<<< HEAD
	password := os.Getenv("OSS_ADMIN_PASSWORD")
	if password == "" {
		// 不设置预置密码时，交给第一个网页注册用户成为管理员。
		return false, nil
	}
	username := cfg.Auth.EffectiveBootstrapAdminUsername()
=======
	username := cfg.Auth.EffectiveBootstrapAdminUsername()
	password := os.Getenv("OSS_ADMIN_PASSWORD")
	if password == "" {
		if input == nil || !term.IsTerminal(int(input.Fd())) {
			return false, errors.New(
				"数据库中没有管理员；非交互式启动请设置 OSS_ADMIN_PASSWORD（至少 8 个字符）",
			)
		}
		var err error
		password, err = promptPassword(input, output, username)
		if err != nil {
			return false, err
		}
	}
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if err := ValidateAccountInput(username, password); err != nil {
		return false, fmt.Errorf("管理员初始化失败: %w", err)
	}
	if _, err := CreateAccount(db, username, password, "admin"); err != nil {
		return false, fmt.Errorf("创建初始管理员失败: %w", err)
	}
	return true, nil
}
<<<<<<< HEAD
=======

func promptPassword(input *os.File, output io.Writer, username string) (string, error) {
	if output == nil {
		output = io.Discard
	}
	fmt.Fprintf(output, "\n[OSS] 首次启动：请为管理员 %q 设置密码（至少 8 个字符）\n", username)
	fmt.Fprint(output, "[OSS] 管理员密码: ")
	first, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(output)
	if err != nil {
		return "", fmt.Errorf("读取管理员密码失败: %w", err)
	}
	fmt.Fprint(output, "[OSS] 再次输入密码: ")
	second, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(output)
	if err != nil {
		return "", fmt.Errorf("读取管理员确认密码失败: %w", err)
	}
	password := string(first)
	if password != string(second) {
		return "", errors.New("两次输入的管理员密码不一致")
	}
	if strings.TrimSpace(password) == "" {
		return "", errors.New("管理员密码不能为空")
	}
	return password, nil
}
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
