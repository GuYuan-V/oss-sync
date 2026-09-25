package settingspolicy

import (
	"errors"
	"fmt"
	"strings"
)

// SyncMode 表示 Vault 的轮询模式
type SyncMode string

const (
	SyncModeUserChoice SyncMode = "user_choice"
	SyncModeShortPoll  SyncMode = "short_poll"
	SyncModeLongPoll   SyncMode = "long_poll"
)

// ErrInvalidSyncMode 表示同步模式不受支持
var ErrInvalidSyncMode = errors.New("invalid sync mode")

// ParseSyncMode 解析用户提交的轮询模式
func ParseSyncMode(value string) (SyncMode, error) {
	mode := SyncMode(strings.TrimSpace(value))
	switch mode {
	case SyncModeUserChoice, SyncModeShortPoll, SyncModeLongPoll:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidSyncMode, value)
	}
}
