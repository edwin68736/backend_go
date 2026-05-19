package cmd

import (
	"fmt"
	"os"

	"tukifac/pkg/database"
)

func printTenantProgress(slug string, err error) {
	fmt.Printf("Migrating tenant: %s\n", slug)
	if err != nil {
		fmt.Printf("✗ failed: %v\n", err)
	} else {
		fmt.Println("✓ completed")
	}
}

// RunMigrate central + todos los tenants activos (lotes).
func RunMigrate() int {
	fmt.Println("Migrating central database...")
	if err := database.RunCentralMigration(); err != nil {
		fmt.Printf("✗ central failed: %v\n", err)
		return 1
	}
	fmt.Println("✓ central migrated")
	fmt.Println()
	fmt.Println("Migrating tenant databases (active tenants)...")
	return printTenantsSummary(database.MigrateTenantsBatch(true, printTenantProgress))
}

// RunMigrateCentral solo BD central.
func RunMigrateCentral() int {
	fmt.Println("Migrating central database...")
	if err := database.RunCentralMigration(); err != nil {
		fmt.Printf("✗ central failed: %v\n", err)
		return 1
	}
	fmt.Println("✓ central migrated")
	fmt.Println()
	fmt.Println("Migration completed (central only).")
	return 0
}

// RunMigrateTenants solo tenants activos en lotes.
func RunMigrateTenants() int {
	fmt.Println("Migrating tenant databases (active tenants)...")
	return printTenantsSummary(database.MigrateTenantsBatch(true, printTenantProgress))
}

// RunMigrateTenant un tenant por slug.
func RunMigrateTenant(slug string) int {
	fmt.Printf("Migrating tenant: %s\n", slug)
	if err := database.MigrateTenantBySlug(slug); err != nil {
		fmt.Printf("✗ failed: %v\n", err)
		return 1
	}
	fmt.Println("✓ completed")
	return 0
}

func printTenantsSummary(summary database.MigrateSummary) int {
	if len(summary.Failed) > 0 && len(summary.Failed) == 1 && summary.Failed[0].Slug == "(list)" {
		return 1
	}

	fmt.Println()
	fmt.Println("Migration completed")
	fmt.Printf("Tenants migrated: %d\n", len(summary.Success))

	if len(summary.Failed) == 0 {
		return 0
	}

	fmt.Println()
	fmt.Println("SUCCESS:")
	fmt.Printf("  %d\n", len(summary.Success))
	fmt.Println()
	fmt.Println("FAILED:")
	fmt.Printf("  %d\n", len(summary.Failed))
	fmt.Println()
	fmt.Println("FAILED TENANTS:")
	for _, f := range summary.Failed {
		fmt.Printf("  - %s\n", f.Slug)
	}
	return 1
}

// InitDatabase conecta la BD central (sin migrar). Para servidor y CLI.
func InitDatabase() error {
	return database.ConnectCentral()
}

// AutoMigrateDev ejecuta migrate completo si AUTO_MIGRATE_DEV=true.
func AutoMigrateDev() error {
	if os.Getenv("AUTO_MIGRATE_DEV") != "true" && os.Getenv("AUTO_MIGRATE_DEV") != "1" {
		return nil
	}
	fmt.Println("[dev] AUTO_MIGRATE_DEV: running migrations...")
	if err := database.RunCentralMigration(); err != nil {
		return err
	}
	summary := database.MigrateTenantsBatch(false, nil)
	if len(summary.Failed) > 0 {
		return fmt.Errorf("tenant migrations failed: %d", len(summary.Failed))
	}
	fmt.Printf("[dev] migrated %d tenants\n", len(summary.Success))
	return nil
}
