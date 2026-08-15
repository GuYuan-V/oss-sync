package webui

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultaccess"
)

type collaborationArticleRow struct {
	CollaborationID uint
	Path            string
}

type collaborationMemberRow struct {
	UserID   uint
	Username string
	Articles []collaborationArticleRow
}

type membersData struct {
	VaultID   string
	VaultName string
	CanManage bool
	Members   []collaborationMemberRow
	Error     string
	Saved     bool
}

func (h *Handler) membersPage(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	d := membersData{
		VaultID: vault.ID, VaultName: vault.Name,
		CanManage: vaultaccess.CanManage(role),
		Error:     c.Query("error"), Saved: c.Query("saved") == "1",
	}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)

	var rows []models.Collaboration
	if err := h.DB.Where("vault_id = ? AND status = ?", vault.ID, collaboration.StatusAccepted).
		Order("collaborator_id asc, created_at asc").Find(&rows).Error; err != nil {
		d.Error = h.t(c, "err.load_members_failed")
		h.renderVaultStatus(c, http.StatusInternalServerError, ld, "vault-members", h.t(c, "page.vault_members", vault.Name), d)
		return
	}

	userIDs := make([]uint, 0, len(rows))
	fileIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.CollaboratorID)
		fileIDs = append(fileIDs, row.FileID)
	}
	usernames := map[uint]string{}
	if len(userIDs) > 0 {
		var users []models.User
		if err := h.DB.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			d.Error = h.t(c, "err.load_members_failed")
			h.renderVaultStatus(c, http.StatusInternalServerError, ld, "vault-members", h.t(c, "page.vault_members", vault.Name), d)
			return
		}
		for _, user := range users {
			usernames[user.ID] = user.Username
		}
	}
	paths := map[uint]string{}
	if len(fileIDs) > 0 {
		var files []models.File
		if err := h.DB.Where("id IN ?", fileIDs).Find(&files).Error; err != nil {
			d.Error = h.t(c, "err.load_collaboration_articles_failed")
			h.renderVaultStatus(c, http.StatusInternalServerError, ld, "vault-members", h.t(c, "page.vault_members", vault.Name), d)
			return
		}
		for _, file := range files {
			paths[file.ID] = file.Path
		}
	}

	grouped := map[uint]*collaborationMemberRow{}
	for _, row := range rows {
		member := grouped[row.CollaboratorID]
		if member == nil {
			member = &collaborationMemberRow{UserID: row.CollaboratorID, Username: usernames[row.CollaboratorID]}
			grouped[row.CollaboratorID] = member
		}
		member.Articles = append(member.Articles, collaborationArticleRow{
			CollaborationID: row.ID,
			Path:            paths[row.FileID],
		})
	}
	for _, member := range grouped {
		sort.Slice(member.Articles, func(i, j int) bool {
			return member.Articles[i].Path < member.Articles[j].Path
		})
		d.Members = append(d.Members, *member)
	}
	sort.Slice(d.Members, func(i, j int) bool {
		return d.Members[i].Username < d.Members[j].Username
	})
	h.renderVault(c, ld, "vault-members", h.t(c, "page.vault_members", vault.Name), d)
}

func (h *Handler) revokeMemberCollaborations(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	redirect := "/dashboard/vaults/" + vault.ID + "/members"
	if !vaultaccess.CanManage(role) {
		c.Redirect(http.StatusSeeOther, redirect+"?error="+url.QueryEscape(h.t(c, "err.no_permission")))
		return
	}
	collaboratorID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusSeeOther, redirect+"?error="+url.QueryEscape(h.t(c, "err.invalid_collaborator")))
		return
	}
	query := h.DB.Model(&models.Collaboration{}).Where(
		"vault_id = ? AND collaborator_id = ? AND status = ?",
		vault.ID, uint(collaboratorID), collaboration.StatusAccepted,
	)
	if c.PostForm("all") != "1" {
		ids := make([]uint, 0, len(c.PostFormArray("collaboration_ids")))
		for _, raw := range c.PostFormArray("collaboration_ids") {
			id, parseErr := strconv.ParseUint(raw, 10, 64)
			if parseErr == nil && id > 0 {
				ids = append(ids, uint(id))
			}
		}
		if len(ids) == 0 {
			c.Redirect(http.StatusSeeOther, redirect+"?error="+url.QueryEscape(h.t(c, "err.select_articles_to_revoke")))
			return
		}
		query = query.Where("id IN ?", ids)
	}
	if err := query.Update("status", collaboration.StatusRevoked).Error; err != nil {
		c.Redirect(http.StatusSeeOther, redirect+"?error="+url.QueryEscape(h.t(c, "err.revoke_collaboration_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, redirect+"?saved=1")
}
