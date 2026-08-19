package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/models"
)

type webCollaborationMembersFixture struct {
	router       *gin.Engine
	db           *gorm.DB
	vault        models.Vault
	collaborator *models.User
	rows         []models.Collaboration
	session      *http.Cookie
	csrf         *http.Cookie
}

func newWebCollaborationMembersFixture(t *testing.T) webCollaborationMembersFixture {
	t.Helper()
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	ownerToken := registerAndLogin(t, router, "collaboration-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	session, csrf := webLogin(t, router, "collaboration-owner", "password123")

	var vault models.Vault
	if err := db.Where("id = ?", vaultID).First(&vault).Error; err != nil {
		t.Fatal(err)
	}
	collaborator, err := auth.CreateAccount(db, "collaboration-writer", "password123", "user")
	if err != nil {
		t.Fatal(err)
	}
	files := []models.File{
		{UserID: vault.OwnerID, VaultID: vault.ID, Path: "Guide.md", Type: "markdown", Revision: 1},
		{UserID: vault.OwnerID, VaultID: vault.ID, Path: "Notes/Plan.md", Type: "markdown", Revision: 2},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	rows := []models.Collaboration{
		{VaultID: vault.ID, FileID: files[0].ID, OwnerID: vault.OwnerID, CollaboratorID: collaborator.ID, Status: collaboration.StatusAccepted},
		{VaultID: vault.ID, FileID: files[1].ID, OwnerID: vault.OwnerID, CollaboratorID: collaborator.ID, Status: collaboration.StatusAccepted},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return webCollaborationMembersFixture{
		router: router, db: db, vault: vault, collaborator: collaborator,
		rows: rows, session: session, csrf: csrf,
	}
}

func TestWebConsoleCollaborationMembers_whenOpened_groupsAcceptedArticlesByCollaborator(t *testing.T) {
	// Given
	fixture := newWebCollaborationMembersFixture(t)

	// When
	page := doForm(t, fixture.router, http.MethodGet,
		"/dashboard/vaults/"+fixture.vault.ID+"/members", nil, fixture.session, fixture.csrf)

	// Then
	if page.Code != http.StatusOK {
		t.Fatalf("members page status = %d, want %d", page.Code, http.StatusOK)
	}
	body := page.Body.String()
	for _, want := range []string{
		fixture.collaborator.Username,
		"2 篇",
		`data-modal-open="collaboration-member-` + strconv.FormatUint(uint64(fixture.collaborator.ID), 10) + `"`,
		`name="collaboration_ids" value="` + strconv.FormatUint(uint64(fixture.rows[0].ID), 10) + `"`,
		`data-select-all`,
		`data-clear-selection`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("collaboration members page missing %q", want)
		}
	}
}

func TestWebConsoleCollaborationMembers_whenSelectedRevoked_scopesRowsToVaultAndCollaborator(t *testing.T) {
	// Given
	fixture := newWebCollaborationMembersFixture(t)
	other := models.Collaboration{
		VaultID: "other-vault", FileID: fixture.rows[0].FileID,
		OwnerID: fixture.vault.OwnerID, CollaboratorID: fixture.collaborator.ID,
		Status: collaboration.StatusAccepted,
	}
	if err := fixture.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}

	// When
	response := doForm(t, fixture.router, http.MethodPost,
		"/dashboard/vaults/"+fixture.vault.ID+"/members/"+
			strconv.FormatUint(uint64(fixture.collaborator.ID), 10)+"/collaborations/revoke",
		url.Values{"collaboration_ids": {
			strconv.FormatUint(uint64(fixture.rows[0].ID), 10),
			strconv.FormatUint(uint64(other.ID), 10),
		}}, fixture.session, fixture.csrf)

	// Then
	if response.Code != http.StatusSeeOther {
		t.Fatalf("selected revoke status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	assertCollaborationStatus(t, fixture.db, fixture.rows[0].ID, collaboration.StatusRevoked)
	assertCollaborationStatus(t, fixture.db, fixture.rows[1].ID, collaboration.StatusAccepted)
	assertCollaborationStatus(t, fixture.db, other.ID, collaboration.StatusAccepted)
}

func TestWebConsoleCollaborationMembers_whenAllRevoked_revokesEveryAcceptedArticle(t *testing.T) {
	// Given
	fixture := newWebCollaborationMembersFixture(t)

	// When
	response := doForm(t, fixture.router, http.MethodPost,
		"/dashboard/vaults/"+fixture.vault.ID+"/members/"+
			strconv.FormatUint(uint64(fixture.collaborator.ID), 10)+"/collaborations/revoke",
		url.Values{"all": {"1"}}, fixture.session, fixture.csrf)

	// Then
	if response.Code != http.StatusSeeOther {
		t.Fatalf("all revoke status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	for _, row := range fixture.rows {
		assertCollaborationStatus(t, fixture.db, row.ID, collaboration.StatusRevoked)
	}
}

func assertCollaborationStatus(t *testing.T, db *gorm.DB, id uint, want string) {
	t.Helper()
	var row models.Collaboration
	if err := db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != want {
		t.Fatalf("collaboration %d status = %q, want %q", id, row.Status, want)
	}
}
