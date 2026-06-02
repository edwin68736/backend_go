package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tukifac/pkg/saas"
	"tukifac/pkg/storagepaths"
	"tukifac/pkg/uploadlimits"

	"github.com/gofiber/fiber/v3"
)

// UploadQR POST /api/superadmin/saas-settings/upload-qr (multipart: kind=yape|plin, file)
func (h *SettingsHandler) UploadQR(c fiber.Ctx) error {
	kind := strings.ToLower(strings.TrimSpace(c.FormValue("kind")))
	if kind != "yape" && kind != "plin" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "kind debe ser yape o plin"})
	}
	file, err := c.FormFile("file")
	if err != nil || file == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "archivo requerido"})
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
	if !allowed[ext] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "formato no permitido (jpg, png, webp)"})
	}
	if file.Size > uploadlimits.MaxFileBytes {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "archivo máximo 10 MB"})
	}

	dir := storagepaths.SaasDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("no se pudo crear carpeta %s: %v", dir, err),
		})
	}
	filename := fmt.Sprintf("qr_%s%s", kind, ext)
	savePath := filepath.Join(dir, filename)
	if err := c.SaveFile(file, savePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("error guardando QR en %s: %v", savePath, err),
		})
	}

	publicURL := "/storage/saas/" + filename
	cfg, err := saas.LoadSettings()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if kind == "yape" {
		cfg.YapeQRURL = publicURL
	} else {
		cfg.PlinQRURL = publicURL
	}
	if err := saas.SaveSettings(cfg); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"kind":    kind,
		"url":     publicURL,
	})
}
