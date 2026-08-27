package auth_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/models"
)

func TestConcurrentFirstRegistration_OnlyOneAdmin(t *testing.T) {
	db := newAuthTestDB(t)
	var wg sync.WaitGroup
	roles := make([]string, 10)
	errs := make([]error, 10)
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			username := fmt.Sprintf("user%d", idx)
			u, err := auth.CreateAccountForAnonymousRegistration(db, username, "password123")
			if err != nil {
				errs[idx] = err
				roles[idx] = fmt.Sprintf("err:%v", err)
				return
			}
			roles[idx] = u.Role
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v roles=%v", err, roles)
		}
	}
	adminCount := 0
	for _, r := range roles {
		if r == "admin" {
			adminCount++
		}
	}
	if adminCount != 1 {
		t.Fatalf("expected exactly 1 admin, got %d roles=%v", adminCount, roles)
	}
	var dbAdminCount int64
	if err := db.Model(&models.User{}).Where("role = ?", "admin").Count(&dbAdminCount).Error; err != nil {
		t.Fatalf("count admin: %v", err)
	}
	if dbAdminCount != 1 {
		t.Fatalf("db admin count = %d, want 1", dbAdminCount)
	}
}
