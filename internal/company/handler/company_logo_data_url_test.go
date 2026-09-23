package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/tenantstorage"
)

// logoFilenameFromURL se movió a pkg/branchlogo (dataurl.go) — su test vive ahí ahora
// (pkg/branchlogo/dataurl_test.go), ya que attachLogoDataURL/attachBranchLogoDataURL en este
// paquete son solo wrappers finos sobre branchlogo.ResolveCompanyDataURL/ResolveBranchDataURL.

func TestAttachLogoDataURL(t *testing.T) {
	ruc := "10726187938"
	dir := tenantstorage.TenantUploadDir(ruc, "company")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	logoPath := filepath.Join(dir, "logo.png")
	// PNG mínimo válido (cabecera + IHDR); basta para comprobar el embebido.
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0x03}
	if err := os.WriteFile(logoPath, png, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(logoPath) })

	t.Run("embebe el logo desde disco", func(t *testing.T) {
		cfg := &database.TenantCompanyConfig{LogoURL: "/uploads/tenants/" + ruc + "/company/logo.png?v=123"}
		attachLogoDataURL(ruc, cfg)
		if !strings.HasPrefix(cfg.LogoDataURL, "data:image/png;base64,") {
			t.Fatalf("esperaba un data URL png, got %q", cfg.LogoDataURL)
		}
		if cfg.LogoURL == "" {
			t.Error("logo_url debe conservarse como respaldo")
		}
	})

	t.Run("config antigua ya embebida se respeta", func(t *testing.T) {
		cfg := &database.TenantCompanyConfig{LogoURL: "data:image/png;base64,AAAA"}
		attachLogoDataURL(ruc, cfg)
		if cfg.LogoDataURL != "data:image/png;base64,AAAA" {
			t.Errorf("esperaba devolver el data URL tal cual, got %q", cfg.LogoDataURL)
		}
	})

	t.Run("sin logo no embebe nada", func(t *testing.T) {
		cfg := &database.TenantCompanyConfig{}
		attachLogoDataURL(ruc, cfg)
		if cfg.LogoDataURL != "" {
			t.Errorf("esperaba vacío, got %q", cfg.LogoDataURL)
		}
	})

	t.Run("archivo inexistente no rompe", func(t *testing.T) {
		cfg := &database.TenantCompanyConfig{LogoURL: "/uploads/tenants/" + ruc + "/company/noexiste.png"}
		attachLogoDataURL(ruc, cfg)
		if cfg.LogoDataURL != "" {
			t.Errorf("esperaba vacío, got %q", cfg.LogoDataURL)
		}
	})

	t.Run("nil no rompe", func(t *testing.T) {
		attachLogoDataURL(ruc, nil)
	})
}
