// 概览页面
package webui

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/models"
)

type overviewData struct {
	VaultCount         int64
	PendingDevices     int64
	DeviceCount        int64
	FileCount          int64
	CPUCores           int
	CPUModelName       string
	CPUUsagePercent    float64
	MemoryBytes        int64
	MemoryTotalBytes   int64
	MemoryUsagePercent float64
	StorageUsed        int64
	StorageQuota       int64
	RecentHistory      []models.FileHistory
	RecentDevices      []recentDeviceRow
}

type recentDeviceRow struct {
	DeviceName string
	VaultName  string
	LastSyncAt time.Time
}

func (h *Handler) overviewPage(c *gin.Context) {
	data, err := h.loadOverviewData(h.webUser(c))
	if err != nil {
		h.render(c, http.StatusInternalServerError, "overview", h.t(c, "page.overview"), "", "overview", data)
		return
	}
	h.render(c, http.StatusOK, "overview", h.t(c, "page.overview"), "", "overview", data)
}

func (h *Handler) loadOverviewData(user *models.User) (overviewData, error) {
	data := overviewData{CPUCores: runtime.NumCPU()}
	metrics := readSystemMetrics(h.Cfg.Storage.DataDir)
	data.CPUUsagePercent = metrics.CPUUsagePercent
	data.CPUModelName = metrics.CPUModelName
	data.MemoryBytes = metrics.MemoryUsedBytes
	data.MemoryTotalBytes = metrics.MemoryTotalBytes
	data.MemoryUsagePercent = metrics.MemoryUsagePercent

	var ownedIDs []string
	if err := h.DB.Model(&models.Vault{}).Where("owner_id = ?", user.ID).
		Pluck("id", &ownedIDs).Error; err != nil {
		return data, fmt.Errorf("list owned vaults: %w", err)
	}
	var members []models.VaultMember
	if err := h.DB.Where("user_id = ?", user.ID).Find(&members).Error; err != nil {
		return data, fmt.Errorf("list vault memberships: %w", err)
	}
	vaultSet := make(map[string]struct{}, len(ownedIDs)+len(members))
	for _, vaultID := range ownedIDs {
		vaultSet[vaultID] = struct{}{}
	}
	for _, member := range members {
		vaultSet[member.VaultID] = struct{}{}
	}
	var ownedVaults []models.Vault
	if err := h.DB.Where("owner_id = ?", user.ID).Find(&ownedVaults).Error; err != nil {
		return data, fmt.Errorf("list owned vault storage: %w", err)
	}
	for _, vault := range ownedVaults {
		data.StorageUsed += vault.StorageUsed
		if vault.StorageQuota == 0 {
			data.StorageQuota = 0
			break
		}
		data.StorageQuota += vault.StorageQuota
	}
	vaultIDs := make([]string, 0, len(vaultSet))
	for vaultID := range vaultSet {
		vaultIDs = append(vaultIDs, vaultID)
	}
	data.VaultCount = int64(len(vaultIDs))

	if err := h.DB.Model(&models.ClientDevice{}).
		Where("user_id = ? AND status = ? AND revoked_at IS NULL", user.ID, "pending").
		Count(&data.PendingDevices).Error; err != nil {
		return data, fmt.Errorf("count pending devices: %w", err)
	}
	if err := h.DB.Model(&models.ClientDevice{}).
		Where("user_id = ?", user.ID).Count(&data.DeviceCount).Error; err != nil {
		return data, fmt.Errorf("count devices: %w", err)
	}
	if len(vaultIDs) == 0 {
		return data, nil
	}
	if err := h.DB.Model(&models.File{}).
		Where("vault_id IN ? AND is_deleted = ?", vaultIDs, false).
		Count(&data.FileCount).Error; err != nil {
		return data, fmt.Errorf("count files: %w", err)
	}
	if err := h.DB.Where("vault_id IN ? AND username = ?", vaultIDs, user.Username).
		Order("created_at desc").Limit(10).
		Find(&data.RecentHistory).Error; err != nil {
		return data, fmt.Errorf("list recent history: %w", err)
	}

	var syncs []models.DeviceVault
	if err := h.DB.Where("user_id = ? AND vault_id IN ?", user.ID, vaultIDs).
		Order("last_sync_at desc").Limit(8).Find(&syncs).Error; err != nil {
		return data, fmt.Errorf("list recent device syncs: %w", err)
	}
	deviceNames := make(map[string]string, len(syncs))
	for _, sync := range syncs {
		if _, exists := deviceNames[sync.ClientID]; exists {
			continue
		}
		var device models.ClientDevice
		if err := h.DB.Where("user_id = ? AND client_id = ?", user.ID, sync.ClientID).
			First(&device).Error; err == nil {
			deviceNames[sync.ClientID] = device.Name
		}
	}
	var vaults []models.Vault
	if err := h.DB.Where("id IN ?", vaultIDs).Find(&vaults).Error; err != nil {
		return data, fmt.Errorf("list vault names: %w", err)
	}
	vaultNames := make(map[string]string, len(vaults))
	for _, vault := range vaults {
		vaultNames[vault.ID] = vault.Name
	}
	for _, sync := range syncs {
		data.RecentDevices = append(data.RecentDevices, recentDeviceRow{
			DeviceName: deviceNames[sync.ClientID],
			VaultName:  vaultNames[sync.VaultID],
			LastSyncAt: sync.LastSyncAt,
		})
	}
	return data, nil
}

func usagePercent(used, total float64) float64 {
	if total <= 0 || used <= 0 {
		return 0
	}
	if used >= total {
		return 100
	}
	return used / total * 100
}

