package webui

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/history"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/vaultaccess"
)

// historyDiffLine 是文本差异中的一行
// Prefix 为原始前缀符号，Kind 取 added、removed 或 context
type historyDiffLine struct {
	Prefix string
	Text   string
	Kind   string
}

// historyDetailData 是修改记录详情页的显示数据
type historyDetailData struct {
	VaultID      string
	ID           uint
	FilePath     string
	PreviousPath string
	ActionLabel  string
	Version      int
	Revision     int64
	Username     string
	DeviceName   string
	CreatedAt    time.Time
	HasSnapshot  bool
	CanRestore   bool
	IsText       bool
	Diff         []historyDiffLine
}

// historyDetailPage 渲染单条修改记录详情：GET /dashboard/vaults/:vault_id/history/:history_id
// 记录不存在返回 404，快照读取失败返回 500
// 文本文件且存在快照时生成与上一版本的差异，无上一版本时以空内容为基准
// 恢复入口只对有管理权限的成员开放
func (h *Handler) historyDetailPage(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	historyID, err := strconv.ParseUint(c.Param("history_id"), 10, 64)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid history id")
		return
	}
	var row models.FileHistory
	if err := h.DB.Where("id = ? AND vault_id = ?", historyID, vault.ID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusNotFound, "history not found")
			return
		}
		c.String(http.StatusInternalServerError, "history unavailable")
		return
	}

	d := historyDetailData{
		VaultID: vault.ID, ID: row.ID, FilePath: row.FilePath, PreviousPath: row.PreviousPath,
		ActionLabel: historyActionLabel(row.Action), Version: row.Version, Revision: row.Revision,
		Username: row.Username, DeviceName: row.DeviceName, CreatedAt: row.CreatedAt,
		CanRestore: vaultaccess.CanManage(role), IsText: history.IsText(row.FilePath),
	}
	snapshot, err := history.ReadSnapshot(h.Cfg.Storage.DataDir, row.ContentKey)
	if err != nil {
		c.String(http.StatusInternalServerError, "history snapshot unreadable")
		return
	}
	d.HasSnapshot = snapshot != nil
	if d.IsText && snapshot != nil {
		var previous models.FileHistory
		base := []byte(nil)
		if row.Version > 1 {
			err := h.DB.Where("vault_id = ? AND file_path = ? AND version = ?", vault.ID, row.FilePath, row.Version-1).
				First(&previous).Error
			if err == nil {
				base, err = history.ReadSnapshot(h.Cfg.Storage.DataDir, previous.ContentKey)
				if err != nil {
					c.String(http.StatusInternalServerError, "previous history snapshot unreadable")
					return
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				c.String(http.StatusInternalServerError, "previous history unavailable")
				return
			}
		}
		for _, line := range history.DiffLines(base, snapshot) {
			d.Diff = append(d.Diff, newHistoryDiffLine(line))
		}
	}

	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	h.renderVault(c, ld, "vault-history-detail", h.t(c, "page.vault_history_detail", vault.Name), d)
}

// newHistoryDiffLine 把一行差异文本解析为带类型标记的展示行
// 首字符为 + 或 - 分别标记 added 与 removed，其余情况统一按上下文行处理并去除首字符
func newHistoryDiffLine(line string) historyDiffLine {
	if line == "" {
		return historyDiffLine{Kind: "context"}
	}
	prefix := line[:1]
	switch prefix {
	case "+":
		return historyDiffLine{Prefix: prefix, Text: line[1:], Kind: "added"}
	case "-":
		return historyDiffLine{Prefix: prefix, Text: line[1:], Kind: "removed"}
	default:
		return historyDiffLine{Prefix: " ", Text: line[1:], Kind: "context"}
	}
}
