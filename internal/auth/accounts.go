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

// IssueToken 为用户签发与 API 登录相同的 JWT。
func IssueToken(cfg *config.Config, user models.User) (string, int64, error) {
	ttl := time.Duration(cfg.Auth.JWTTTLHours) * time.Hour
	token, err := jwt.Sign(cfg.Auth.JWTSecret, jwt.Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, ttl)
	if err != nil {
		return "", 0, err
	}
	return token, int64(ttl / time.Second), nil
}
