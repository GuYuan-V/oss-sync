// 系统指标
package webui

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/storagequota"
)

type systemMetrics struct {
	CPUModelName       string
	CPUUsagePercent    float64
	MemoryUsedBytes    int64
	MemoryTotalBytes   int64
	MemoryUsagePercent float64
	DiskUsedBytes      int64
	DiskTotalBytes     int64
	ProjectStorageUsed int64
	ProjectStorageMax  int64
}

type systemMetricsResponse struct {
	CPUModelName       string  `json:"cpu_model_name"`
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsedBytes    int64   `json:"memory_used_bytes"`
	MemoryTotalBytes   int64   `json:"memory_total_bytes"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	DiskUsedBytes      int64   `json:"disk_used_bytes"`
	DiskTotalBytes     int64   `json:"disk_total_bytes"`
	ProjectStorageUsed int64   `json:"project_storage_used"`
	ProjectStorageMax  int64   `json:"project_storage_max"`
	VaultStorageUsed   int64   `json:"vault_storage_used"`
	VaultStorageQuota  int64   `json:"vault_storage_quota"`
}

type cpuSample struct {
	total uint64
	idle  uint64
	at    time.Time
}

var cpuSampler struct {
	sync.Mutex
	previous cpuSample
}

func readSystemMetrics(dataDir string, projectStorageMax int64) systemMetrics {
	current := readCPUSample()
	cpuSampler.Lock()
	previous := cpuSampler.previous
	cpuSampler.previous = current
	cpuSampler.Unlock()
	if previous.at.IsZero() {
		previous = current
		time.Sleep(200 * time.Millisecond)
		current = readCPUSample()
		cpuSampler.Lock()
		cpuSampler.previous = current
		cpuSampler.Unlock()
	}
	memoryUsed, memoryTotal := memoryUsage()
	diskUsed, diskTotal := diskUsage(dataDir)
	projectStorageUsed, _ := storagequota.Usage(dataDir)
	return systemMetrics{
		CPUModelName:       cpuModelName(),
		CPUUsagePercent:    cpuUsage(previous, current),
		MemoryUsedBytes:    memoryUsed,
		MemoryTotalBytes:   memoryTotal,
		MemoryUsagePercent: usagePercent(float64(memoryUsed), float64(memoryTotal)),
		DiskUsedBytes:      diskUsed,
		DiskTotalBytes:     diskTotal,
		ProjectStorageUsed: projectStorageUsed,
		ProjectStorageMax:  projectStorageMax,
	}
}

func (h *Handler) systemMetricsPage(c *gin.Context) {
	user := h.webUser(c)
	metrics := readSystemMetrics(h.Cfg.Storage.DataDir, h.Cfg.Storage.MaxTotalSizeBytes())
	response := systemMetricsResponse{
		CPUModelName:       metrics.CPUModelName,
		CPUUsagePercent:    metrics.CPUUsagePercent,
		MemoryUsedBytes:    metrics.MemoryUsedBytes,
		MemoryTotalBytes:   metrics.MemoryTotalBytes,
		MemoryUsagePercent: metrics.MemoryUsagePercent,
		DiskUsedBytes:      metrics.DiskUsedBytes,
		DiskTotalBytes:     metrics.DiskTotalBytes,
		ProjectStorageUsed: metrics.ProjectStorageUsed,
		ProjectStorageMax:  metrics.ProjectStorageMax,
	}

	if err := h.DB.Model(&models.Vault{}).Where("owner_id = ?", user.ID).
		Select("COALESCE(SUM(storage_used), 0), COALESCE(SUM(storage_quota), 0)").
		Row().Scan(&response.VaultStorageUsed, &response.VaultStorageQuota); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load live metrics"})
		return
	}

	if user.Role == "admin" {
		if err := h.DB.Model(&models.Vault{}).Select("COALESCE(SUM(storage_used), 0)").
			Row().Scan(&response.VaultStorageUsed); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "load live metrics"})
			return
		}
		response.VaultStorageQuota = 0
	}
	c.JSON(http.StatusOK, response)
}

func cpuUsage(previous, current cpuSample) float64 {
	if current.total <= previous.total || current.idle < previous.idle {
		return 0
	}
	total := current.total - previous.total
	idle := current.idle - previous.idle
	if total <= idle {
		return 0
	}
	return usagePercent(float64(total-idle), float64(total))
}
