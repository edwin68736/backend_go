package tenantstorage

import (
	"os"
	"path/filepath"
	"strings"
)

// DeleteUploadByPublicURL elimina un archivo local referenciado por URL relativa (/uploads/...).
// Ignora URLs vacías, http(s) y data URLs.
func DeleteUploadByPublicURL(publicURL string) error {
	u := strings.TrimSpace(publicURL)
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "data:") {
		return nil
	}
	rel := strings.TrimPrefix(u, "/")
	if strings.Contains(rel, "..") || !strings.HasPrefix(rel, UploadsRoot+"/") {
		return nil
	}
	full, err := filepath.Abs(filepath.Join(".", filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	uploadsRoot, err := filepath.Abs(UploadsRoot)
	if err != nil {
		return nil
	}
	if full != uploadsRoot && !strings.HasPrefix(full, uploadsRoot+string(os.PathSeparator)) {
		return nil
	}
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(full)
}
