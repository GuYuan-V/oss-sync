package settingspolicy

import (
	"errors"
	"fmt"
	"strings"
)

type SyncMode string

const (
	SyncModeUserChoice SyncMode = "user_choice"
	SyncModeShortPoll  SyncMode = "short_poll"
	SyncModeLongPoll   SyncMode = "long_poll"
)

var ErrInvalidSyncMode = errors.New("invalid sync mode")

func ParseSyncMode(value string) (SyncMode, error) {
	mode := SyncMode(strings.TrimSpace(value))
	switch mode {
	case SyncModeUserChoice, SyncModeShortPoll, SyncModeLongPoll:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidSyncMode, value)
	}
}
