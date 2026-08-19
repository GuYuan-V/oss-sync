package settingspolicy

import (
	"testing"

	"github.com/oss/oss-server/internal/models"
)

func TestResolve_whenPreferencesExceedLimits_clampsToAdministratorCeilings(t *testing.T) {
	t.Parallel()

	// Given
	system := models.SystemSetting{
		DefaultRecycleBinDays: 30,
		MaxLongPollWaitSec:    20,
		MaxSyncDebounceSec:    60,
		MaxRecycleBinDays:     90,
		MaxVaultStorageBytes:  10 << 30,
		MaxUploadSizeBytes:    50 << 20,
	}
	user := models.UserSetting{
		LongPollWaitSec:       30,
		SyncDebounceSec:       120,
		DefaultRecycleBinDays: 180,
		VaultStorageBytes:     20 << 30,
		UploadSizeBytes:       100 << 20,
	}

	// When
	policy := Resolve(system, user, 100<<20)

	// Then
	if policy.LongPollWaitSec != 20 {
		t.Errorf("long poll wait = %d, want 20", policy.LongPollWaitSec)
	}
	if policy.SyncDebounceSec != 60 {
		t.Errorf("sync debounce = %d, want 60", policy.SyncDebounceSec)
	}
	if policy.RecycleBinDays != 90 {
		t.Errorf("recycle days = %d, want 90", policy.RecycleBinDays)
	}
	if policy.VaultStorageBytes != 10<<30 {
		t.Errorf("vault storage = %d, want %d", policy.VaultStorageBytes, int64(10<<30))
	}
	if policy.UploadSizeBytes != 50<<20 {
		t.Errorf("upload size = %d, want %d", policy.UploadSizeBytes, int64(50<<20))
	}
}

func TestResolve_whenPreferencesAreUnset_usesSafeDefaultsAndInheritedLimits(t *testing.T) {
	t.Parallel()

	// Given
	system := models.SystemSetting{
		DefaultRecycleBinDays: 45,
		MaxVaultStorageBytes:  5 << 30,
	}

	// When
	policy := Resolve(system, models.UserSetting{}, 100<<20)

	// Then
	if policy.LongPollWaitSec != 30 || policy.SyncDebounceSec != 3 {
		t.Errorf("poll/debounce = %d/%d, want 30/3", policy.LongPollWaitSec, policy.SyncDebounceSec)
	}
	if policy.RecycleBinDays != 45 {
		t.Errorf("recycle days = %d, want 45", policy.RecycleBinDays)
	}
	if policy.VaultStorageBytes != 5<<30 {
		t.Errorf("vault storage = %d, want %d", policy.VaultStorageBytes, int64(5<<30))
	}
	if policy.UploadSizeBytes != 100<<20 {
		t.Errorf("upload size = %d, want %d", policy.UploadSizeBytes, int64(100<<20))
	}
}

func TestValidatePreferences_whenValueExceedsLimit_returnsError(t *testing.T) {
	t.Parallel()

	// Given
	limits := Limits{
		LongPollWaitSec:   20,
		SyncDebounceSec:   60,
		RecycleBinDays:    90,
		VaultStorageBytes: 10 << 30,
		UploadSizeBytes:   50 << 20,
	}
	preferences := Preferences{LongPollWaitSec: 21}

	// When
	err := ValidatePreferences(preferences, limits)

	// Then
	if err == nil {
		t.Fatal("ValidatePreferences returned nil, want ceiling error")
	}
}
