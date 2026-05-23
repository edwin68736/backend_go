package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v031User struct {
	HomeBranchID         *uint `gorm:"column:home_branch_id"`
	CanSwitchBranch      bool  `gorm:"column:can_switch_branch"`
	BranchSessionVersion uint  `gorm:"column:branch_session_version"`
}

func (v031User) TableName() string { return "tenant_users" }

type v031Floor struct {
	BranchID uint `gorm:"column:branch_id"`
}

func (v031Floor) TableName() string { return "tenant_restaurant_floors" }

type v031Table struct {
	BranchID uint `gorm:"column:branch_id"`
}

func (v031Table) TableName() string { return "tenant_restaurant_tables" }

// V031MultiBranch DDL multi-sucursal (sin UPDATE masivos).
type V031MultiBranch struct{}

func (V031MultiBranch) Version() int { return 31 }
func (V031MultiBranch) Name() string { return "multi_branch_schema" }

func (V031MultiBranch) Up(db *gorm.DB) error {
	mig := db.Migrator()

	u := &v031User{}
	if mig.HasTable(u) {
		if !mig.HasColumn(u, "HomeBranchID") {
			if err := mig.AddColumn(u, "HomeBranchID"); err != nil {
				return fmt.Errorf("add tenant_users.home_branch_id: %w", err)
			}
		}
		if !mig.HasColumn(u, "CanSwitchBranch") {
			if err := mig.AddColumn(u, "CanSwitchBranch"); err != nil {
				return fmt.Errorf("add tenant_users.can_switch_branch: %w", err)
			}
		}
		if !mig.HasColumn(u, "BranchSessionVersion") {
			if err := mig.AddColumn(u, "BranchSessionVersion"); err != nil {
				return fmt.Errorf("add tenant_users.branch_session_version: %w", err)
			}
		}
	}

	floor := &v031Floor{}
	if mig.HasTable(floor) && !mig.HasColumn(floor, "BranchID") {
		if err := mig.AddColumn(floor, "BranchID"); err != nil {
			return fmt.Errorf("add tenant_restaurant_floors.branch_id: %w", err)
		}
	}

	tbl := &v031Table{}
	if mig.HasTable(tbl) && !mig.HasColumn(tbl, "BranchID") {
		if err := mig.AddColumn(tbl, "BranchID"); err != nil {
			return fmt.Errorf("add tenant_restaurant_tables.branch_id: %w", err)
		}
	}

	return nil
}
