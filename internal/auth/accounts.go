// 账户管理
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/deviceauth"
	"github.com/oss/oss-server/internal/jwt"
	"github.com/oss/oss-server/internal/models"
)

// ValidateAccountInput 对 API 和网页注册使用同一套账号规则。
func ValidateAccountInput(username, password string) error {
	usernameLength := utf8.RuneCountInString(strings.TrimSpace(username))
	passwordLength := utf8.RuneCountInString(password)
	if usernameLength < 3 || usernameLength > 64 {
		return errors.New("用户名需要 3-64 个字符")
	}
	if passwordLength < 8 {
		return errors.New("密码至少需要 8 个字符")
	}
	// bcrypt 只接受最多 72 字节；明确拒绝，避免长密码被误报为用户名冲突。
	if len([]byte(password)) > 72 {
		return errors.New("密码的 UTF-8 编码不能超过 72 字节")
	}
	return nil
}

// ResolveRegistrationRole 在注册锁内调用：系统中还没有管理员时，
// 第一个注册账户自动成为 admin，之后一律为 user。
func ResolveRegistrationRole(db *gorm.DB) (string, error) {
	var adminCount int64
	if err := db.Model(&models.User{}).Where("role = ?", "admin").Count(&adminCount).Error; err != nil {
		return "", err
	}
	if adminCount == 0 {
		return "admin", nil
	}
	return "user", nil
}

// CreateAccount 创建用户及默认用户设置。Vault 必须由用户登录后手动创建。
func CreateAccount(db *gorm.DB, username, password, role string) (*models.User, error) {
	username = strings.TrimSpace(username)
	role = strings.ToLower(strings.TrimSpace(role))
	if err := ValidateAccountInput(username, password); err != nil {
		return nil, err
	}
	if role != "admin" && role != "user" {
		return nil, errors.New("role 必须为 admin 或 user")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("生成密码哈希失败: %w", err)
	}
	user := models.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		StorageQuota: 0,
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.UserSetting{UserID: user.ID}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// AuthenticateCredentials 校验用户名密码，并返回当前数据库中的用户。
func AuthenticateCredentials(db *gorm.DB, username, password string) (*models.User, error) {
	var user models.User
	if err := db.Where("username = ?", strings.TrimSpace(username)).First(&user).Error; err != nil {
		return nil, errBadCred
	}
	if user.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, errBadCred
	}
	return &user, nil
}

// AuthenticateToken 校验网页管理面板 cookie 中保存的 JWT。
func AuthenticateToken(db *gorm.DB, cfg *config.Config, token string) (*models.User, error) {
	return authenticateBearer(db, cfg, token)
}

// AuthenticateIdentityToken validates a bearer token and returns its optional device binding.
func AuthenticateIdentityToken(db *gorm.DB, cfg *config.Config, token string) (*Identity, error) {
	return authenticateBearerIdentity(db, cfg, token)
}

// IssueToken 为用户签发与 API 登录相同的 JWT。
func IssueToken(cfg *config.Config, user models.User) (string, int64, error) {
	return issueToken(cfg, jwt.Claims{
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
		TokenVersion: user.TokenVersion,
	})
}

// IssueDeviceToken 为指定设备签发绑定 did 的 JWT，复用与 IssueToken 相同的签名逻辑。
func IssueDeviceToken(cfg *config.Config, user models.User, deviceID jwt.DeviceID) (string, int64, error) {
	normalized := deviceauth.NormalizeClientID(string(deviceID))
	if normalized == "" {
		return "", 0, errors.New("invalid device id")
	}
	return issueToken(cfg, jwt.Claims{
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
		TokenVersion: user.TokenVersion,
		DeviceID:     jwt.DeviceID(normalized),
	})
}

func issueToken(cfg *config.Config, claims jwt.Claims) (string, int64, error) {
	ttl := time.Duration(cfg.Auth.JWTTTLHours) * time.Hour
	token, err := jwt.Sign(cfg.Auth.JWTSecret, claims, ttl)
	if err != nil {
		return "", 0, err
	}
	return token, int64(ttl / time.Second), nil
}

// ChangePassword 校验旧密码后更新用户密码并递增 token 版本。
// 所有旧 JWT 在版本递增后立即失效。
func ChangePassword(db *gorm.DB, userID uint, oldPassword, newPassword string) error {
	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		return err
	}
	if user.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)) != nil {
		return errBadCred
	}
	if err := ValidateAccountInput(user.Username, newPassword); err != nil {
		return err
	}
	return updatePassword(db, userID, newPassword)
}

// SetPassword 由管理员调用，重置目标用户密码并递增 token 版本。
func SetPassword(db *gorm.DB, userID uint, newPassword string) error {
	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		return err
	}
	if err := ValidateAccountInput(user.Username, newPassword); err != nil {
		return err
	}
	return updatePassword(db, userID, newPassword)
}

func updatePassword(db *gorm.DB, userID uint, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return db.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash": string(hash),
		"token_version": gorm.Expr("token_version + 1"),
	}).Error
}

