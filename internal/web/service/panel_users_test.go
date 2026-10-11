package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
)

func TestPanelUsersRolesAndGuards(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	var first model.User
	if err := db.Order("id asc").First(&first).Error; err != nil {
		hash, _ := crypto.HashPasswordAsBcrypt("admin-pass")
		first = model.User{Username: "admin", Password: hash}
		db.Create(&first)
	}
	legacy := model.User{Username: "legacy", Password: "x"}
	db.Create(&legacy)
	if RoleOf(first.Id) != model.RoleAdmin || RoleOf(legacy.Id) != model.RoleAdmin {
		t.Fatal("existing users must keep full access")
	}

	s := &PanelUserService{}
	if _, err := s.Add("op", "short", model.RoleOperator); err == nil {
		t.Fatal("short passwords must be refused")
	}
	if _, err := s.Add("op", "long-enough", "root"); err == nil {
		t.Fatal("unknown roles must be refused")
	}
	op, err := s.Add("op", "long-enough", model.RoleOperator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("op", "long-enough", model.RoleOperator); err == nil {
		t.Fatal("duplicate usernames must be refused")
	}
	if RoleOf(op.Id) != model.RoleOperator {
		t.Fatal("new operator must be an operator")
	}

	var before model.User
	db.First(&before, op.Id)
	if err := s.Update(op.Id, "new-password-1", model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	var after model.User
	db.First(&after, op.Id)
	if RoleOf(op.Id) != model.RoleAdmin || after.LoginEpoch != before.LoginEpoch+1 || !crypto.CheckPasswordHash(after.Password, "new-password-1") {
		t.Fatalf("update must change role and password and sign the user out: %+v", after)
	}
	// A stray operator row can never demote the primary admin.
	db.Save(&model.PanelRole{UserId: first.Id, Role: model.RoleOperator})
	if RoleOf(first.Id) != model.RoleAdmin {
		t.Fatal("the primary admin must always be admin")
	}
	if err := s.Update(first.Id, "", model.RoleOperator); err == nil {
		t.Fatal("the primary admin is not editable here")
	}
	if err := s.Delete(first.Id, op.Id); err == nil {
		t.Fatal("the primary admin cannot be deleted")
	}
	if err := s.Delete(op.Id, op.Id); err == nil {
		t.Fatal("users cannot delete themselves")
	}
	if err := s.Delete(op.Id, first.Id); err != nil {
		t.Fatal(err)
	}
	users, _ := s.List()
	for _, u := range users {
		if u.Id == op.Id {
			t.Fatal("deleted user still listed")
		}
	}
}
