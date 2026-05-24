package fiscal

import "strings"

// NormalizeSunatEnvMode unifica valores legacy (beta) a demo | production.
func NormalizeSunatEnvMode(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "production" {
		return "production"
	}
	return "demo"
}

// SunatEnvToFacturadorAmbiente mapea demo/production → pruebas/produccion (Lycet).
func SunatEnvToFacturadorAmbiente(sunatEnvMode string) string {
	if NormalizeSunatEnvMode(sunatEnvMode) == "production" {
		return "produccion"
	}
	return "pruebas"
}
