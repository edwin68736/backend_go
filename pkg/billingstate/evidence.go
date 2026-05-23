package billingstate

import (
	"strings"

	"tukifac/pkg/database"
	"tukifac/pkg/facturador"
)

// Evidence evidencia mínima para afirmar aceptación SUNAT.
type Evidence struct {
	HasSignedXML bool
	HasCDR       bool
	HasHash      bool
	CDRCode      string
	SunatSuccess bool
	CDRAccepted  bool
	HasTicket    bool
}

// EvaluateLycet evalúa respuesta Lycet (HTTP 200 no implica SUNAT aceptó).
func EvaluateLycet(resp *facturador.SunatResponse, xmlURL, cdrURL string) (pipeline string, ev Evidence) {
	ev = Evidence{}
	if resp == nil {
		return FAILED, ev
	}
	if strings.TrimSpace(resp.XML) != "" || strings.TrimSpace(xmlURL) != "" {
		ev.HasSignedXML = true
	}
	if strings.TrimSpace(resp.Hash) != "" {
		ev.HasHash = true
	}
	if strings.TrimSpace(resp.CDRZipBase64()) != "" || strings.TrimSpace(cdrURL) != "" {
		ev.HasCDR = true
	}
	if resp.SunatResponse != nil {
		ev.SunatSuccess = resp.SunatResponse.Success
		ev.HasTicket = strings.TrimSpace(resp.SunatResponse.Ticket) != ""
		if resp.SunatResponse.CDRResponse != nil {
			ev.CDRCode = strings.TrimSpace(resp.SunatResponse.CDRResponse.Code)
			ev.CDRAccepted = resp.SunatResponse.CDRResponse.Accepted
		}
	}
	if resp.ConnectionError() != "" {
		return FAILED, ev
	}
	if !ev.SunatSuccess && resp.SunatResponse != nil && resp.SunatResponse.Error != nil {
		return FAILED, ev
	}
	if ev.SunatSuccess && ev.CDRCode == "" && !ev.HasCDR {
		// Facturador respondió pero sin CDR: estado intermedio, no éxito.
		if ev.HasSignedXML {
			return FACTURADOR_RECEIVED, ev
		}
		return SENDING_TO_SUNAT, ev
	}
	if IsAcceptanceEvidence(ev) {
		if len(resp.CDRNotes()) > 0 && ev.CDRCode == "0" {
			return OBSERVED, ev
		}
		return SUNAT_ACCEPTED, ev
	}
	if ev.CDRCode != "" && ev.CDRCode != "0" {
		return SUNAT_REJECTED, ev
	}
	if ev.SunatSuccess && !ev.CDRAccepted && ev.CDRCode != "0" {
		return SUNAT_REJECTED, ev
	}
	if ev.HasSignedXML || ev.SunatSuccess {
		return SENDING_TO_SUNAT, ev
	}
	return FAILED, ev
}

// IsAcceptanceEvidence regla de oro: CDR código 0 + XML firmado + CDR almacenado.
func IsAcceptanceEvidence(ev Evidence) bool {
	return ev.CDRCode == "0" && ev.CDRAccepted && ev.HasSignedXML && ev.HasCDR && ev.SunatSuccess
}

// HasAcceptanceEvidence desde registro persistido.
func HasAcceptanceEvidence(inv *database.TenantInvoice) bool {
	if inv == nil {
		return false
	}
	ev := Evidence{
		HasSignedXML: strings.TrimSpace(inv.XMLURL) != "" || strings.TrimSpace(inv.SunatHash) != "",
		HasCDR:       strings.TrimSpace(inv.CDRURL) != "" && inv.CDRURL != "(CDR recibido)",
		HasHash:      strings.TrimSpace(inv.SunatHash) != "",
		CDRCode:      strings.TrimSpace(inv.SunatCDRCode),
		SunatSuccess: strings.TrimSpace(inv.SunatStatus) == "accepted" || inv.SunatCDRCode == "0",
		CDRAccepted:  inv.SunatCDRCode == "0",
	}
	return IsAcceptanceEvidence(ev)
}

// EvaluatePSE respuesta PSE (generarenviar): exige XML firmado + CDR + hash.
func EvaluatePSE(isSuccess bool, codigoHash, xmlB64, cdrB64 string, xmlSaved, cdrSaved bool) string {
	hasXML := (strings.TrimSpace(xmlB64) != "" || xmlSaved)
	hasCDR := (strings.TrimSpace(cdrB64) != "" || cdrSaved)
	hasHash := strings.TrimSpace(codigoHash) != ""
	if !isSuccess {
		return SUNAT_REJECTED
	}
	if isSuccess && hasXML && hasCDR && hasHash {
		return SUNAT_ACCEPTED
	}
	if isSuccess && hasXML {
		return FACTURADOR_RECEIVED
	}
	return FAILED
}
