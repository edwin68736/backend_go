package tenantstorage

import (
	"os"
	"path/filepath"
	"strings"

	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
)

// UploadsHandler sirve archivos bajo /uploads con compatibilidad slug ↔ RUC en la ruta.
func UploadsHandler(c fiber.Ctx) error {
	rel := strings.TrimPrefix(c.Path(), "/uploads/")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
		return c.Status(fiber.StatusNotFound).SendString("Not Found")
	}

	full := filepath.Join(UploadsRoot, filepath.FromSlash(rel))
	if st, err := os.Stat(full); err == nil && !st.IsDir() {
		return c.SendFile(full)
	}

	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != "tenants" {
		return c.Status(fiber.StatusNotFound).SendString("Not Found")
	}
	segment := parts[1]
	tenant, ok := findTenantByUploadSegment(segment)
	if !ok {
		return c.Status(fiber.StatusNotFound).SendString("Not Found")
	}

	ruc := SanitizeRUC(tenant.RUC)
	slug := strings.TrimSpace(tenant.Slug)
	candidates := []string{}
	if ruc != "" {
		candidates = append(candidates, ruc)
	}
	if slug != "" {
		candidates = append(candidates, slug)
	}
	if segment != "" {
		candidates = append(candidates, segment)
	}

	seen := map[string]bool{}
	for _, id := range candidates {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		parts[1] = id
		alt := filepath.Join(UploadsRoot, filepath.Join(parts...))
		if st, err := os.Stat(alt); err == nil && !st.IsDir() {
			return c.SendFile(alt)
		}
	}

	return c.Status(fiber.StatusNotFound).SendString("Not Found")
}

func findTenantByUploadSegment(segment string) (*database.Tenant, bool) {
	var tenant database.Tenant
	if err := database.CentralDB.Where("slug = ?", segment).First(&tenant).Error; err == nil {
		return &tenant, true
	}
	if ruc := SanitizeRUC(segment); ruc != "" {
		if err := database.CentralDB.Where("ruc = ?", ruc).First(&tenant).Error; err == nil {
			return &tenant, true
		}
	}
	return nil, false
}
