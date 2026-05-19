// Package tenantstorage define rutas de archivos por tenant usando el RUC de la empresa.
//
// Estructura:
//
//	uploads/tenants/{RUC}/products/
//	uploads/tenants/{RUC}/contacts/
//	uploads/tenants/{RUC}/receipts/
//
//	storage/invoices/tenants/{RUC}/{proveedor}/{xml|cdr|signed|pdf}/  (facturación)
package tenantstorage

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	UploadsRoot          = "uploads"
	UploadsTenantsPrefix = "uploads/tenants"
)

// SanitizeRUC deja solo dígitos del RUC (carpeta por empresa).
func SanitizeRUC(ruc string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(ruc) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SanitizePathSegment evita caracteres inválidos en nombres de carpeta (proveedor PSE, etc.).
func SanitizePathSegment(v string) string {
	v = filepath.Clean(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "\\", "_")
	v = strings.ReplaceAll(v, "/", "_")
	v = strings.Trim(v, "._- ")
	if v == "" {
		return "default"
	}
	return v
}

// TenantUploadDir ruta en disco: uploads/tenants/{ruc}/{subdir}.
func TenantUploadDir(ruc, subdir string) string {
	return filepath.Join(UploadsRoot, "tenants", SanitizeRUC(ruc), subdir)
}

// TenantUploadPublicURL URL pública: /uploads/tenants/{ruc}/{subdir}/{filename}.
func TenantUploadPublicURL(ruc, subdir, filename string) string {
	r := SanitizeRUC(ruc)
	return fmt.Sprintf("/%s/%s/%s/%s", UploadsTenantsPrefix, r, subdir, filename)
}

// InvoiceTenantDir ruta en disco para comprobantes: {base}/tenants/{ruc}/...
func InvoiceTenantDir(basePath, ruc, provider, kind string) string {
	return filepath.Join(basePath, "tenants", SanitizeRUC(ruc), SanitizePathSegment(provider), kind)
}
