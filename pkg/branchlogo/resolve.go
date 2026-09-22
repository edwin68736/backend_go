// Package branchlogo resuelve qué logo usar en un comprobante: el logo propio de la
// sucursal emisora si lo tiene, o si no el logo global de la empresa (TenantCompanyConfig).
package branchlogo

import "strings"

// ResolveURL es el ÚNICO punto que decide esta cadena de fallback: nadie más debe repetirla.
// Mismo patrón override+fallback que pkg/saleunit.ResolvePrice usa para precios por sucursal.
func ResolveURL(branchLogoURL, companyLogoURL string) string {
	if url := strings.TrimSpace(branchLogoURL); url != "" {
		return url
	}
	return companyLogoURL
}
