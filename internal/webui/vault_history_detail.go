// 历史详情
package webui

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/history"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultaccess"
)

type historyDiffLine struct {
	Prefix string
	Text   string
	Kind   string
}

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

