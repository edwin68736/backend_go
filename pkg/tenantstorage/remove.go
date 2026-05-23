package tenantstorage

import (
	"fmt"
	"os"
	"path/filepath"
)

// RemoveAllTenantFiles borra carpetas locales del tenant (uploads, facturas, comprobantes SaaS).
// No toca el facturador Lycet/SUNAT externo.
func RemoveAllTenantFiles(tenantID uint, ruc, invoiceStorageBase string) (removed []string, errs []error) {
	r := SanitizeRUC(ruc)
	if r != "" {
		removed, errs = appendRemove(removed, errs, filepath.Join(UploadsRoot, "tenants", r))
		if invoiceStorageBase != "" {
			removed, errs = appendRemove(removed, errs, filepath.Join(invoiceStorageBase, "tenants", r))
		}
	}
	tidDir := fmt.Sprintf("tenant_%d", tenantID)
	for _, sub := range []string{"receipts", "doc_packages"} {
		removed, errs = appendRemove(removed, errs, filepath.Join("storage", "saas", sub, tidDir))
	}
	return removed, errs
}

func appendRemove(removed []string, errs []error, path string) ([]string, []error) {
	if path == "" || path == "." || path == string(filepath.Separator) {
		return removed, errs
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return removed, errs
	}
	if err := os.RemoveAll(path); err != nil {
		return removed, append(errs, fmt.Errorf("%s: %w", path, err))
	}
	return append(removed, path), errs
}
