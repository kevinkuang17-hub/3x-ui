package service

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
)

// PanelUserService manages extra panel logins and their roles; the original first user is
// untouched and always admin, so single-user panels behave exactly as before.
type PanelUserService struct{}

type PanelUser struct {
	Id       int    `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Primary  bool   `json:"primary"`
}

func firstUserID() int {
	var u model.User
	if err := database.GetDB().Order("id asc").Select("id").First(&u).Error; err != nil {
		return 0
	}
	return u.Id
}

// RoleOf returns the user's role. Users without a role row are admins, as every panel user was
// before roles existed; users created here always get a row in the same transaction.
func RoleOf(userID int) string {
	var r model.PanelRole
	if err := database.GetDB().Where("user_id = ?", userID).First(&r).Error; err == nil && r.Role == model.RoleOperator && userID != firstUserID() {
		return model.RoleOperator
	}
	return model.RoleAdmin
}

func (s *PanelUserService) List() ([]PanelUser, error) {
	var users []model.User
	if err := database.GetDB().Order("id asc").Find(&users).Error; err != nil {
		return nil, err
	}
	first := firstUserID()
	out := make([]PanelUser, 0, len(users))
	for _, u := range users {
		out = append(out, PanelUser{Id: u.Id, Username: u.Username, Role: RoleOf(u.Id), Primary: u.Id == first})
	}
	return out, nil
}

func validRole(role string) bool { return role == model.RoleAdmin || role == model.RoleOperator }

func (s *PanelUserService) Add(username, password, role string) (*PanelUser, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(password) < 8 {
		return nil, common.NewError("username is required and the password needs at least 8 characters")
	}
	if !validRole(role) {
		return nil, common.NewError("role must be admin or operator")
	}
	db := database.GetDB()
	var count int64
	db.Model(&model.User{}).Where("username = ?", username).Count(&count)
	if count > 0 {
		return nil, common.NewError("username already exists")
	}
	hash, err := crypto.HashPasswordAsBcrypt(password)
	if err != nil {
		return nil, err
	}
	u := &model.User{Username: username, Password: hash}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		return tx.Create(&model.PanelRole{UserId: u.Id, Role: role}).Error
	})
	if err != nil {
		return nil, err
	}
	return &PanelUser{Id: u.Id, Username: u.Username, Role: role}, nil
}

// Update changes an extra user's role and/or password; both sign the user out everywhere.
func (s *PanelUserService) Update(id int, password, role string) error {
	if id == firstUserID() {
		return common.NewError("the primary admin is managed in Settings → Security")
	}
	db := database.GetDB()
	var u model.User
	if err := db.Where("id = ?", id).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewError("user not found")
		}
		return err
	}
	updates := map[string]any{"login_epoch": gorm.Expr("login_epoch + 1")}
	if password != "" {
		if len(password) < 8 {
			return common.NewError("the password needs at least 8 characters")
		}
		hash, err := crypto.HashPasswordAsBcrypt(password)
		if err != nil {
			return err
		}
		updates["password"] = hash
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if role != "" {
			if !validRole(role) {
				return common.NewError("role must be admin or operator")
			}
			if err := tx.Save(&model.PanelRole{UserId: id, Role: role}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error
	})
}

func (s *PanelUserService) Delete(id, actingUserID int) error {
	if id == firstUserID() {
		return common.NewError("the primary admin cannot be deleted")
	}
	if id == actingUserID {
		return common.NewError("you cannot delete the user you are signed in as")
	}
	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&model.PanelRole{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.User{}, id).Error
	})
}
