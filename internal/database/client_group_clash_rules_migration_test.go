package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Legacy client_groups schema without clash_rules — the upgrade path AddColumn must cover.
const legacyClientGroupNoClashRulesDDL = "CREATE TABLE `client_groups` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`reset_up` integer DEFAULT 0,`reset_down` integer DEFAULT 0,`created_at` integer,`updated_at` integer)"

func TestMigrateClientGroupClashRulesColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "x-ui.db")
	legacy, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := legacy.Exec(legacyClientGroupNoClashRulesDDL).Error; err != nil {
		t.Fatalf("create legacy client_groups: %v", err)
	}
	if err := legacy.Exec(`INSERT INTO client_groups (name, reset_up, reset_down) VALUES ('vip', 7, 9)`).Error; err != nil {
		t.Fatalf("seed legacy group: %v", err)
	}
	sqlDB, err := legacy.DB()
	if err != nil {
		t.Fatalf("legacy db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB over legacy schema: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	if !GetDB().Migrator().HasColumn(&model.ClientGroup{}, "clash_rules") {
		t.Fatal("clash_rules column missing after migration")
	}
	var row model.ClientGroup
	if err := GetDB().Where("name = ?", "vip").First(&row).Error; err != nil {
		t.Fatalf("preexisting group lost: %v", err)
	}
	if row.ClashRules != "" || row.ResetUp != 7 || row.ResetDown != 9 {
		t.Fatalf("preexisting group changed: %+v", row)
	}
	if err := migrateClientGroupClashRulesColumn(); err != nil {
		t.Fatalf("idempotent migrate: %v", err)
	}
}
