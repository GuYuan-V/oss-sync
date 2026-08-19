package webui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/deviceauth"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultaccess"
	"github.com/oss/oss-server/internal/vaultbackup"
)

// 备份管理

type adminBackupRow struct {
	ID        string
	VaultName string
	Owner     string
	FileName  string
	Size      string
	CreatedAt time.Time
}

func (h *Handler) backupRows() ([]adminBackupRow, error) {
	var backups []models.VaultBackup
	if err := h.DB.Order("created_at desc").Find(&backups).Error; err != nil {
		return nil, err
	}
	ownerIDs := make([]uint, 0, len(backups))
	for _, backup := range backups {
		ownerIDs = append(ownerIDs, backup.OwnerID)
	}
	owners := map[uint]string{}
	if len(ownerIDs) > 0 {
		var users []models.User
		if err := h.DB.Unscoped().Where("id IN ?", ownerIDs).Find(&users).Error; err == nil {
			for _, u := range users {
				owners[u.ID] = u.Username
			}
		}
	}
	rows := make([]adminBackupRow, 0, len(backups))
	for _, backup := range backups {
		rows = append(rows, adminBackupRow{
			ID: backup.ID, VaultName: backup.VaultName, Owner: owners[backup.OwnerID],
			FileName: backup.FileName, Size: formatBytes(backup.Size), CreatedAt: backup.CreatedAt,
		})
	}
	return rows, nil
}

func (h *Handler) downloadBackup(c *gin.Context) {
	var backup models.VaultBackup
	if err := h.DB.Where("id = ?", c.Param("id")).First(&backup).Error; err != nil {
		c.String(http.StatusNotFound, "backup not found")
		return
	}
	path, err := vaultbackup.Path(backup.FileName)
	if err != nil {
		c.String(http.StatusNotFound, "backup not found")
		return
	}
	if _, err := os.Stat(path); err != nil {
		c.String(http.StatusNotFound, "backup archive is missing")
		return
	}
	c.FileAttachment(path, filepath.Base(path))
}

func (h *Handler) deleteBackup(c *gin.Context) {
	var backup models.VaultBackup
	if err := h.DB.Where("id = ?", c.Param("id")).First(&backup).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/system")
		return
	}
	path, err := vaultbackup.Path(backup.FileName)
	if err == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			c.String(http.StatusInternalServerError, "failed to delete backup archive")
			return
		}
	}
	if err := h.DB.Delete(&backup).Error; err != nil {
		c.String(http.StatusInternalServerError, "failed to delete backup record")
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/system?saved=1")
}

// 用户管理

type adminUserRow struct {
	UserID      uint
	Username    string
	Role        string
	CreatedAt   time.Time
	VaultCount  int64
	DeviceCount int64
	IsLastAdmin bool
}

type adminUsersData struct {
	UserCount  int
	AdminCount int
	Users      []adminUserRow
	Error      string
	Saved      bool
}

func (h *Handler) adminUsersPage(c *gin.Context) {
	d := adminUsersData{Error: c.Query("error"), Saved: c.Query("saved") == "1"}
	var users []models.User
	if err := h.DB.Order("created_at asc, id asc").Find(&users).Error; err != nil {
		h.render(c, http.StatusInternalServerError, "admin-users", h.t(c, "page.admin_users"), "admin", "admin-users", d)
		return
	}
	d.UserCount = len(users)
	adminCount := 0
	for _, u := range users {
		if u.Role == "admin" {
			adminCount++
		}
	}
	d.AdminCount = adminCount
	lastAdminID := h.lastAdminID(users)
	for _, u := range users {
		var vaultCount int64
		var deviceCount int64
		h.DB.Model(&models.Vault{}).Where("owner_id = ?", u.ID).Count(&vaultCount)
		h.DB.Model(&models.ClientDevice{}).Where("user_id = ?", u.ID).Count(&deviceCount)
		d.Users = append(d.Users, adminUserRow{
			UserID: u.ID, Username: u.Username, Role: u.Role,
			CreatedAt: u.CreatedAt, VaultCount: vaultCount, DeviceCount: deviceCount,
			IsLastAdmin: u.ID == lastAdminID,
		})
	}
	h.render(c, http.StatusOK, "admin-users", h.t(c, "page.admin_users"), "admin", "admin-users", d)
}

func (h *Handler) lastAdminID(users []models.User) uint {
	var last uint
	count := 0
	for _, u := range users {
		if u.Role == "admin" {
			last = u.ID
			count++
		}
	}
	if count <= 1 {
		return last
	}
	return 0 // 不止一个管理员，允许全部操作
}

func (h *Handler) adminSetUserRole(c *gin.Context) {
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	newRole := c.PostForm("role")
	if err != nil || (newRole != "admin" && newRole != "user") {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.invalid_role")))
		return
	}
	if newRole == "user" {
		// 不能降级最后一个管理员。
		var adminCount int64
		h.DB.Model(&models.User{}).Where("role = ?", "admin").Count(&adminCount)
		if adminCount <= 1 {
			c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.cannot_demote_last_admin")))
			return
		}
	}
	result := h.DB.Model(&models.User{}).Where("id = ?", uint(userID)).Update("role", newRole)
	if result.Error != nil || result.RowsAffected == 0 {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.update_role_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin?saved=1")
}

func (h *Handler) adminResetPassword(c *gin.Context) {
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	newPass := c.PostForm("new_password")
	confirm := c.PostForm("new_password_confirm")
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.invalid_user")))
		return
	}
	if newPass == "" || newPass != confirm {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.password_mismatch")))
		return
	}
	if err := auth.ValidateAccountInput("valid-name", newPass); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.password_invalid")))
		return
	}
	if err := auth.SetPassword(h.DB, uint(userID), newPass); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.reset_password_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin?saved=1")
}

func (h *Handler) adminDeleteUser(c *gin.Context) {
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.invalid_user")))
		return
	}
	var target models.User
	if err := h.DB.Where("id = ?", uint(userID)).First(&target).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.user_not_found")))
		return
	}
	if target.Role == "admin" {
		var adminCount int64
		h.DB.Model(&models.User{}).Where("role = ?", "admin").Count(&adminCount)
		if adminCount <= 1 {
			c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.cannot_delete_last_admin")))
			return
		}
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		var vaults []models.Vault
		if err := tx.Where("owner_id = ?", target.ID).Find(&vaults).Error; err != nil {
			return err
		}
		for _, v := range vaults {
			if _, err := vaultbackupPurge(tx, h, v); err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&models.ClientDevice{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&models.VaultMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&models.Share{}).Error; err != nil {
			return err
		}
		return tx.Delete(&target).Error
	}); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin?error="+url.QueryEscape(h.t(c, "err.delete_user_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin?saved=1")
}

// 全部仓库

type adminVaultRow struct {
	ID          string
	Name        string
	Owner       string
	MemberCount int64
	StorageUsed int64
	ThemeName   string
}

type adminVaultsData struct {
	VaultCount int
	Vaults     []adminVaultRow
	Error      string
}

func (h *Handler) adminVaultsPage(c *gin.Context) {
	d := adminVaultsData{Error: c.Query("error")}
	var vaults []models.Vault
	if err := h.DB.Order("created_at desc").Find(&vaults).Error; err != nil {
		h.render(c, http.StatusInternalServerError, "admin-vaults", h.t(c, "page.admin_vaults"), "admin", "admin-vaults", d)
		return
	}
	d.VaultCount = len(vaults)
	ownerIDs := make([]uint, 0, len(vaults))
	for _, v := range vaults {
		ownerIDs = append(ownerIDs, v.OwnerID)
	}
	owners := map[uint]string{}
	if len(ownerIDs) > 0 {
		var users []models.User
		if err := h.DB.Where("id IN ?", ownerIDs).Find(&users).Error; err == nil {
			for _, u := range users {
				owners[u.ID] = u.Username
			}
		}
	}
	settings := map[string]models.VaultSetting{}
	var settingsRows []models.VaultSetting
	if err := h.DB.Where("vault_id IN ?", func() []string {
		ids := make([]string, 0, len(vaults))
		for _, v := range vaults {
			ids = append(ids, v.ID)
		}
		return ids
	}()).Find(&settingsRows).Error; err == nil {
		for _, s := range settingsRows {
			settings[s.VaultID] = s
		}
	}
	for _, v := range vaults {
		var memberCount int64
		h.DB.Model(&models.VaultMember{}).Where("vault_id = ?", v.ID).Count(&memberCount)
		theme := "default"
		if s, ok := settings[v.ID]; ok {
			if s.ThemeName != "" {
				theme = s.ThemeName
			}
		}
		d.Vaults = append(d.Vaults, adminVaultRow{
			ID: v.ID, Name: v.Name, Owner: owners[v.OwnerID],
			MemberCount: memberCount, StorageUsed: v.StorageUsed,
			ThemeName: theme,
		})
	}
	h.render(c, http.StatusOK, "admin-vaults", h.t(c, "page.admin_vaults"), "admin", "admin-vaults", d)
}

// 仓库详情

type adminVaultDetailData struct {
	VaultID     string
	VaultName   string
	Owner       string
	Description string
	ThemeName   string
	StorageUsed int64
	FileCount   int64
	Members     []memberRow
	Devices     []adminDeviceRow
	Error       string
}

type adminDeviceRow struct {
	Username   string
	ClientID   string
	Name       string
	Status     string
	UserID     uint
	LastSeenAt time.Time
	LastCursor int64
	// Vaults 是该设备所属用户可访问（owner 或有效成员）的仓库授权选项。
	Vaults          []vaultOption
	AuthorizedCount int
	AuthorizedNames []string
}

func (h *Handler) adminVaultDetailPage(c *gin.Context) {
	vaultID := c.Param("vault_id")
	var vault models.Vault
	if err := h.DB.Where("id = ?", vaultID).First(&vault).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/vaults?error="+url.QueryEscape(h.t(c, "err.vault_not_found")))
		return
	}
	d := adminVaultDetailData{
		VaultID: vault.ID, VaultName: vault.Name, Description: vault.Description,
		StorageUsed: vault.StorageUsed, Error: c.Query("error"),
	}
	var owner models.User
	if err := h.DB.Where("id = ?", vault.OwnerID).First(&owner).Error; err == nil {
		d.Owner = owner.Username
	}
	var setting models.VaultSetting
	if err := h.DB.Where("vault_id = ?", vault.ID).First(&setting).Error; err == nil {
		d.ThemeName = setting.ThemeName
	}
	if d.ThemeName == "" {
		d.ThemeName = "default"
	}
	h.DB.Model(&models.File{}).Where("vault_id = ? AND is_deleted = ?", vault.ID, false).Count(&d.FileCount)

	// 成员。
	var members []models.VaultMember
	if err := h.DB.Where("vault_id = ?", vault.ID).Order("created_at asc").Find(&members).Error; err == nil {
		ids := make([]uint, 0, len(members))
		for _, m := range members {
			ids = append(ids, m.UserID)
		}
		usernames := map[uint]string{}
		if len(ids) > 0 {
			var users []models.User
			if err := h.DB.Where("id IN ?", ids).Find(&users).Error; err == nil {
				for _, u := range users {
					usernames[u.ID] = u.Username
				}
			}
		}
		for _, m := range members {
			d.Members = append(d.Members, memberRow{UserID: m.UserID, Username: usernames[m.UserID], Role: m.Role})
		}
	}

	// 设备授权。
	var accesses []models.DeviceVaultAccess
	if err := h.DB.Where("vault_id = ?", vault.ID).Find(&accesses).Error; err == nil {
		clientIDs := make([]string, 0, len(accesses))
		userIDs := make([]uint, 0, len(accesses))
		for _, a := range accesses {
			clientIDs = append(clientIDs, a.ClientID)
			userIDs = append(userIDs, a.UserID)
		}
		deviceNames := map[string]string{}
		deviceStatus := map[string]string{}
		if len(clientIDs) > 0 {
			var devs []models.ClientDevice
			if err := h.DB.Where("client_id IN ?", clientIDs).Find(&devs).Error; err == nil {
				for _, dev := range devs {
					deviceNames[dev.ClientID] = dev.Name
					deviceStatus[dev.ClientID] = dev.Status
				}
			}
		}
		usernames := map[uint]string{}
		if len(userIDs) > 0 {
			var users []models.User
			if err := h.DB.Where("id IN ?", userIDs).Find(&users).Error; err == nil {
				for _, u := range users {
					usernames[u.ID] = u.Username
				}
			}
		}
		for _, a := range accesses {
			var dv models.DeviceVault
			cursor := int64(0)
			if err := h.DB.Where("user_id = ? AND client_id = ? AND vault_id = ?",
				a.UserID, a.ClientID, vault.ID).First(&dv).Error; err == nil {
				cursor = dv.LastCursor
			}
			d.Devices = append(d.Devices, adminDeviceRow{
				Username: usernames[a.UserID], ClientID: a.ClientID,
				Name: deviceNames[a.ClientID], Status: deviceStatus[a.ClientID],
				UserID: a.UserID, LastCursor: cursor,
			})
		}
	}

	h.render(c, http.StatusOK, "admin-vault-detail", h.t(c, "page.admin_vault_detail", vault.Name), "admin", "admin-vaults", d)
}

// 全部设备

type adminDevicesData struct {
	Devices []adminDeviceRow
	Error   string
	Saved   bool
}

func (h *Handler) adminDevicesPage(c *gin.Context) {
	d := adminDevicesData{Error: c.Query("error"), Saved: c.Query("saved") == "1"}
	var devices []models.ClientDevice
	if err := h.DB.Where("status <> ?", deviceauth.DeviceStatusRevoked).
		Order("created_at desc").Find(&devices).Error; err != nil {
		h.render(c, http.StatusInternalServerError, "admin-devices", h.t(c, "page.admin_devices"), "admin", "admin-devices", d)
		return
	}
	userIDs := make([]uint, 0, len(devices))
	clientIDs := make([]string, 0, len(devices))
	for _, dev := range devices {
		userIDs = append(userIDs, dev.UserID)
		clientIDs = append(clientIDs, dev.ClientID)
	}
	usernames := map[uint]string{}
	if len(userIDs) > 0 {
		var users []models.User
		if err := h.DB.Where("id IN ?", userIDs).Find(&users).Error; err == nil {
			for _, u := range users {
				usernames[u.ID] = u.Username
			}
		}
	}
	// 全部仓库供授权选择；每个设备只列出其所属用户可访问（owner 或有效成员）的仓库。
	var vaults []models.Vault
	if err := h.DB.Order("name asc").Find(&vaults).Error; err != nil {
		h.render(c, http.StatusInternalServerError, "admin-devices", h.t(c, "page.admin_devices"), "admin", "admin-devices", d)
		return
	}
	accessByClient := map[string]map[string]bool{}
	for _, dev := range devices {
		accessByClient[dev.ClientID] = map[string]bool{}
	}
	if len(clientIDs) > 0 {
		var accesses []models.DeviceVaultAccess
		if err := h.DB.Where("client_id IN ?", clientIDs).Find(&accesses).Error; err == nil {
			for _, a := range accesses {
				if accessByClient[a.ClientID] != nil {
					accessByClient[a.ClientID][a.VaultID] = true
				}
			}
		}
	}
	// 按目标用户过滤可授权仓库：管理员不能通过设备授权绕过用户的 Vault 权限。
	accessible := h.accessibleVaultIDsForUsers(devices)
	for _, dev := range devices {
		var dv models.DeviceVault
		cursor := int64(0)
		if err := h.DB.Where("user_id = ? AND client_id = ?", dev.UserID, dev.ClientID).
			Order("last_sync_at desc").First(&dv).Error; err == nil {
			cursor = dv.LastCursor
		}
		opts := make([]vaultOption, 0, len(vaults))
		for _, v := range vaults {
			if !accessible[dev.UserID][v.ID] {
				continue
			}
			opt := vaultOption{ID: v.ID, Name: v.Name, AuthorizedForClient: map[string]bool{}}
			if accessByClient[dev.ClientID][v.ID] {
				opt.AuthorizedForClient[dev.ClientID] = true
			}
			opts = append(opts, opt)
		}
		authCnt, authNms := deviceAuthSummary(opts, dev.ClientID)
		d.Devices = append(d.Devices, adminDeviceRow{
			Username: usernames[dev.UserID], ClientID: dev.ClientID, Name: dev.Name,
			Status: dev.Status, UserID: dev.UserID, LastSeenAt: dev.LastSeenAt, LastCursor: cursor, Vaults: opts,
			AuthorizedCount: authCnt, AuthorizedNames: authNms,
		})
	}
	h.render(c, http.StatusOK, "admin-devices", h.t(c, "page.admin_devices"), "admin", "admin-devices", d)
}

// accessibleVaultIDsForUsers 返回每个用户可访问（owner 或有效成员）的仓库 ID 集合。
func (h *Handler) accessibleVaultIDsForUsers(devices []models.ClientDevice) map[uint]map[string]bool {
	out := map[uint]map[string]bool{}
	userIDs := make([]uint, 0, len(devices))
	seen := map[uint]bool{}
	for _, dev := range devices {
		if !seen[dev.UserID] {
			seen[dev.UserID] = true
			userIDs = append(userIDs, dev.UserID)
			out[dev.UserID] = map[string]bool{}
		}
	}
	for _, uid := range userIDs {
		var owned []models.Vault
		if err := h.DB.Where("owner_id = ?", uid).Find(&owned).Error; err == nil {
			for _, v := range owned {
				out[uid][v.ID] = true
			}
		}
		var memberships []models.VaultMember
		if err := h.DB.Where("user_id = ?", uid).Find(&memberships).Error; err == nil {
			for _, m := range memberships {
				if vaultaccess.ValidMemberRole(m.Role) {
					out[uid][m.VaultID] = true
				}
			}
		}
	}
	return out
}

// deviceAuthSummary 返回客户端已授权的仓库数量及其名称（保持输入切片顺序）。
func deviceAuthSummary(vaults []vaultOption, clientID string) (int, []string) {
	var names []string
	for _, v := range vaults {
		if v.AuthorizedForClient[clientID] {
			names = append(names, v.Name)
		}
	}
	return len(names), names
}

func (h *Handler) adminAuthorizeDevice(c *gin.Context) {
	admin := h.webUser(c)
	clientID := deviceauth.NormalizeClientID(c.Param("client_id"))
	userID, err := strconv.ParseUint(c.PostForm("user_id"), 10, 64)
	if clientID == "" || err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.invalid_device")))
		return
	}
	var dev models.ClientDevice
	if err := h.DB.Where("user_id = ? AND client_id = ?", uint(userID), clientID).First(&dev).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.device_not_found")))
		return
	}
	name := dev.Name
	if dev.Status == deviceauth.DeviceStatusPending {
		name = strings.TrimSpace(c.PostForm("name"))
		if name == "" {
			name = dev.Name
		} else if len([]rune(name)) > 128 {
			c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.device_name_length")))
			return
		}
	}
	// 状态：pending 表单提交 approved 一并批准；空则保持当前状态。
	status := strings.TrimSpace(c.PostForm("status"))
	if status == "" {
		status = dev.Status
	}
	if status != deviceauth.DeviceStatusApproved {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.invalid_device_status")))
		return
	}
	// 授权仓库必须属于目标用户可访问范围，管理员不能绕过用户的 Vault 权限。
	var wanted []string
	for _, id := range c.PostFormArray("vault_ids") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, _, err := vaultaccess.Resolve(h.DB, uint(userID), id); err != nil {
			c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.inaccessible_vault_for_user")))
			return
		}
		wanted = append(wanted, id)
	}
	if err := h.saveDeviceAuthorization(uint(userID), clientID, name, status, wanted, admin.ID, time.Now()); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.save_authorization_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?saved=1")
}

func (h *Handler) adminRevokeDevice(c *gin.Context) {
	clientID := deviceauth.NormalizeClientID(c.Param("client_id"))
	userID, err := strconv.ParseUint(c.PostForm("user_id"), 10, 64)
	if clientID == "" || err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.invalid_device")))
		return
	}
	if err := deviceauth.RevokeAllDeviceAccesses(h.DB, uint(userID), clientID); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.revoke_failed")))
		return
	}
	now := time.Now()
	if err := h.DB.Model(&models.ClientDevice{}).
		Where("user_id = ? AND client_id = ?", uint(userID), clientID).
		Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?error="+url.QueryEscape(h.t(c, "err.revoke_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/devices?saved=1")
}
