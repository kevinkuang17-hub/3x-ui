package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

const configHistoryKeep = 50

// ConfigHistoryService versions the Xray template config without touching the save path:
// a job snapshots whatever is stored, so edits from the UI, API or bots are all covered.
type ConfigHistoryService struct {
	settingService     SettingService
	xraySettingService XraySettingService
}

func configHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func (s *ConfigHistoryService) latest() (*model.ConfigSnapshot, error) {
	var snap model.ConfigSnapshot
	err := database.GetDB().Order("id desc").First(&snap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &snap, err
}

// Capture stores the current template when it differs from the newest snapshot and returns
// the snapshot describing what is stored now.
func (s *ConfigHistoryService) Capture(now time.Time, note string) (*model.ConfigSnapshot, error) {
	content, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}
	hash := configHash(content)
	last, err := s.latest()
	if err != nil {
		return nil, err
	}
	if last != nil && last.Hash == hash {
		return last, nil
	}
	snap := &model.ConfigSnapshot{Hash: hash, Content: content, Size: len(content), Note: note, CreatedAt: now.Unix()}
	db := database.GetDB()
	if err := db.Create(snap).Error; err != nil {
		return nil, err
	}
	var stale []int
	db.Model(&model.ConfigSnapshot{}).Order("id desc").Offset(configHistoryKeep).Pluck("id", &stale)
	if len(stale) > 0 {
		db.Where("id IN ?", stale).Delete(&model.ConfigSnapshot{})
	}
	return snap, nil
}

func (s *ConfigHistoryService) MarkGood(id int) error {
	return database.GetDB().Model(&model.ConfigSnapshot{}).Where("id = ?", id).Update("good", true).Error
}

func (s *ConfigHistoryService) LastGood(excludeHash string) (*model.ConfigSnapshot, error) {
	var snap model.ConfigSnapshot
	err := database.GetDB().Where("good = ? AND hash <> ?", true, excludeHash).Order("id desc").First(&snap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &snap, err
}

func (s *ConfigHistoryService) List() ([]model.ConfigSnapshot, error) {
	var snaps []model.ConfigSnapshot
	err := database.GetDB().Select("id, hash, size, good, note, created_at").Order("id desc").Find(&snaps).Error
	return snaps, err
}

func (s *ConfigHistoryService) Get(id int) (*model.ConfigSnapshot, error) {
	var snap model.ConfigSnapshot
	if err := database.GetDB().Where("id = ?", id).First(&snap).Error; err != nil {
		return nil, common.NewError("version not found")
	}
	return &snap, nil
}

// Restore saves an older version as the current template (validated like a normal save) and
// records the switch as a new snapshot.
func (s *ConfigHistoryService) Restore(id int, note string) (*model.ConfigSnapshot, error) {
	snap, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := s.xraySettingService.SaveXraySetting(snap.Content); err != nil {
		return nil, err
	}
	cur, err := s.Capture(time.Now(), note)
	if err != nil {
		return nil, err
	}
	// Same content as a version Xray ran with (save may only re-normalise it), so it stays good;
	// otherwise conservative mode would roll back to it again and again.
	if snap.Good {
		_ = s.MarkGood(cur.Id)
	}
	return cur, nil
}
