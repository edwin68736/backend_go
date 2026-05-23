package billingstate

import (
	"testing"

	"tukifac/pkg/facturador"
)

func TestIsAcceptanceEvidence_requiresCDRAndXML(t *testing.T) {
	ev := Evidence{CDRCode: "0", CDRAccepted: true, SunatSuccess: true, HasSignedXML: true, HasCDR: true}
	if !IsAcceptanceEvidence(ev) {
		t.Fatal("expected acceptance")
	}
	ev.HasCDR = false
	if IsAcceptanceEvidence(ev) {
		t.Fatal("must not accept without CDR")
	}
}

func TestEvaluateLycet_rejectsHTTPSuccessWithoutCDR(t *testing.T) {
	resp := &facturador.SunatResponse{
		XML:  "<Invoice/>",
		Hash: "abc",
		SunatResponse: &facturador.SunatResponseInner{
			Success: true,
			CDRResponse: &facturador.SunatCDRResponse{
				Code: "3205", Accepted: false, Description: "rechazo",
			},
		},
	}
	p, _ := EvaluateLycet(resp, "", "")
	if p == SUNAT_ACCEPTED {
		t.Fatalf("got %s, want reject/failed", p)
	}
}

func TestEvaluateLycet_acceptsWithCDR0(t *testing.T) {
	resp := &facturador.SunatResponse{
		XML:  "<Invoice/>",
		Hash: "abc",
		SunatResponse: &facturador.SunatResponseInner{
			Success: true,
			CDRZip:  "base64zip",
			CDRResponse: &facturador.SunatCDRResponse{
				Code: "0", Accepted: true, Description: "aceptado",
			},
		},
	}
	p, ev := EvaluateLycet(resp, "/xml", "/cdr.zip")
	if p != SUNAT_ACCEPTED {
		t.Fatalf("got %s", p)
	}
	if !IsAcceptanceEvidence(ev) {
		t.Fatal("evidence should pass")
	}
}
