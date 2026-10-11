package webui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/history"
	"github.com/helantianshen/oss-sync/internal/models"
)

// historyFilterTimeLayout 是历史筛选时间输入框使用的本地时间格式
const historyFilterTimeLayout = "2006-01-02T15:04"

// historyRow 是修改记录列表一行的显示数据
type historyRow struct {
	ID           uint
	FilePath     string
	PreviousPath string
	Action       string
	ActionLabel  string
	Version      int
	Username     string
	DeviceName   string
	HasSnapshot  bool
	CreatedAt    time.Time
}

// historyFilters 保存历史列表的筛选条件
// Action、Username、Device、From、To 为原始字符串，fromTime 与 toTime 为解析后的本地时间
type historyFilters struct {
	Action   string
	Username string
	Device   string
	From     string
	To       string
	fromTime time.Time
	toTime   time.Time
}

// historyData 是修改记录页的显示数据
type historyData struct {
	VaultID  string
	FilePath string
	Filters  historyFilters
	History  []historyRow
	Error    string
	Page     Page
	PageSize int
}

// paginationPage 返回分页视图所需的当前页信息
func (d historyData) paginationPage() Page { return d.Page }

// paginationBasePath 返回分页链接的基础路径
func (d historyData) paginationBasePath() string {
	return "/dashboard/vaults/" + d.VaultID + "/history"
}

// parseHistoryFilters 解析查询参数中的历史筛选条件
// 空操作类型表示不限；时间必须匹配 historyFilterTimeLayout 且开始时间不得晚于结束时间
func parseHistoryFilters(values url.Values, location *time.Location) (historyFilters, error) {
	filters := historyFilters{
		Action:   strings.TrimSpace(values.Get("action")),
		Username: strings.TrimSpace(values.Get("username")),
		Device:   strings.TrimSpace(values.Get("device")),
		From:     strings.TrimSpace(values.Get("from")),
		To:       strings.TrimSpace(values.Get("to")),
	}
	if !isHistoryAction(filters.Action) {
		return filters, errors.New("操作类型无效")
	}
	if filters.From != "" {
		parsed, err := time.ParseInLocation(historyFilterTimeLayout, filters.From, location)
		if err != nil {
			return filters, errors.New("开始时间格式无效")
		}
		filters.fromTime = parsed
	}
	if filters.To != "" {
		parsed, err := time.ParseInLocation(historyFilterTimeLayout, filters.To, location)
		if err != nil {
			return filters, errors.New("结束时间格式无效")
		}
		filters.toTime = parsed
	}
	if !filters.fromTime.IsZero() && !filters.toTime.IsZero() && filters.fromTime.After(filters.toTime) {
		return filters, errors.New("开始时间不能晚于结束时间")
	}
	return filters, nil
}

// apply 把筛选条件叠加到查询上，空条件不追加 WHERE 子句
// Device 同时匹配设备名称与 client_id，时间条件为闭区间
func (f historyFilters) apply(query *gorm.DB) *gorm.DB {
	if f.Action != "" {
		query = query.Where("action = ?", f.Action)
	}
	if f.Username != "" {
		query = query.Where("username = ?", f.Username)
	}
	if f.Device != "" {
		query = query.Where("device_name = ? OR client_id = ?", f.Device, f.Device)
	}
	if !f.fromTime.IsZero() {
		query = query.Where("created_at >= ?", f.fromTime)
	}
	if !f.toTime.IsZero() {
		query = query.Where("created_at <= ?", f.toTime)
	}
	return query
}

// isHistoryAction 判断操作类型是否为已知历史动作，空字符串视为不限
func isHistoryAction(action string) bool {
	switch action {
	case "", history.ActionCreate, history.ActionModify, history.ActionDelete, history.ActionRestore, history.ActionRename:
		return true
	default:
		return false
	}
}

// historyPage 渲染仓库修改记录页：GET /dashboard/vaults/:vault_id/history
// 筛选参数非法时以 400 渲染错误；path 参数限定单文件历史
// 用户设置读取失败仅记录不存在以外的情况才中断，数据库查询失败返回 500
func (h *Handler) historyPage(c *gin.Context) {
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	filters, err := parseHistoryFilters(c.Request.URL.Query(), time.Local)
	d := historyData{VaultID: vault.ID, Filters: filters, Error: c.Query("error")}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	if err != nil {
		d.Error = err.Error()
		h.renderVaultStatus(c, http.StatusBadRequest, ld, "vault-history", h.t(c, "page.vault_history", vault.Name), d)
		return
	}
	if rawPath := c.Query("path"); rawPath != "" {
		filePath, valid := normalizeWebPath(rawPath)
		if !valid {
			c.String(http.StatusBadRequest, "invalid path")
			return
		}
		d.FilePath = filePath
	}
	user := h.webUser(c)
	var setting models.UserSetting
	if err := h.DB.Where("user_id = ?", user.ID).First(&setting).Error; err != nil && !errorsIsNotFound(err) {
		h.renderVaultStatus(c, http.StatusInternalServerError, ld, "vault-history", h.t(c, "page.vault_history", vault.Name), d)
		return
	}
	d.PageSize = h.userPageSize(c, &setting)
	query := h.DB.Where("vault_id = ?", vault.ID)
	if d.FilePath != "" {
		query = query.Where("file_path = ?", d.FilePath)
	}
	var total int64
	if err := filters.apply(query.Model(&models.FileHistory{})).Count(&total).Error; err != nil {
		h.renderVaultStatus(c, http.StatusInternalServerError, ld, "vault-history", h.t(c, "page.vault_history", vault.Name), d)
		return
	}
	d.Page = h.currentPage(c, total, d.PageSize)
	var rows []models.FileHistory
	if err := filters.apply(query).Order("created_at desc, id desc").
		Limit(d.PageSize).Offset(d.Page.Offset()).Find(&rows).Error; err != nil {
		h.renderVaultStatus(c, http.StatusInternalServerError, ld, "vault-history", h.t(c, "page.vault_history", vault.Name), d)
		return
	}
	for _, row := range rows {
		d.History = append(d.History, historyRow{
			ID: row.ID, FilePath: row.FilePath, PreviousPath: row.PreviousPath,
			Action: row.Action, ActionLabel: historyActionLabel(row.Action), Version: row.Version, Username: row.Username,
			DeviceName: row.DeviceName, HasSnapshot: row.ContentKey != "", CreatedAt: row.CreatedAt,
		})
	}
	h.renderVault(c, ld, "vault-history", h.t(c, "page.vault_history", vault.Name), d)
}

// historyActionLabel 返回历史动作的中文展示名，未知动作原样返回
func historyActionLabel(action string) string {
	switch action {
	case history.ActionCreate:
		return "创建"
	case history.ActionModify:
		return "修改"
	case history.ActionDelete:
		return "删除"
	case history.ActionRestore:
		return "恢复"
	case history.ActionRename:
		return "重命名"
	default:
		return action
	}
}
