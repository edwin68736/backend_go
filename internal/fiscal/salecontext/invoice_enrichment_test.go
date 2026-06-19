package salecontext

import (
	"testing"

	"tukifac/pkg/facturador"
)

func TestEnrichmentFromOutput_andApply(t *testing.T) {
	out := &FiscalContextOutput{
		Profile: FiscalProfileOutput{
			OperationTypeCode:   "0101",
			FiscalObservations:  "Entrega parcial",
			PurchaseOrderNumber: "OC-999",
			HasIgvRetention:     true,
			ShowTermsConditions: true,
		},
		References: []FiscalReferenceOutput{
			{
				ReferenceKind:        RefKindGuiaRemitente,
				ReferencedSunatType:  "09",
				ReferencedFullNumber: "T001-00000001",
			},
		},
		Summary: FiscalSummaryOutput{
			SaleTotal:        1000,
			RetentionAmount:  30,
			NetCollectible:   970,
			RetentionApplied: true,
		},
	}
	e := EnrichmentFromOutput(out)
	if e == nil || e.Compra != "OC-999" || e.Observacion != "Entrega parcial" {
		t.Fatalf("unexpected enrichment: %+v", e)
	}
	if len(e.Guias) != 1 || e.Guias[0].NroDoc != "T001-00000001" {
		t.Fatalf("guias: %+v", e.Guias)
	}

	payload := &facturador.InvoicePayload{TipoOperacion: "0101"}
	ApplyToInvoicePayload(payload, e)
	if payload.Compra != "OC-999" || payload.Observacion != "Entrega parcial" {
		t.Fatalf("payload fields: compra=%q obs=%q", payload.Compra, payload.Observacion)
	}
	if len(payload.Guias) != 1 || payload.Guias[0].NroDoc != "T001-00000001" {
		t.Fatalf("payload guias: %+v", payload.Guias)
	}
}

func TestApplyToInvoicePayload_nilSafe(t *testing.T) {
	ApplyToInvoicePayload(nil, &InvoiceEnrichment{})
	ApplyToInvoicePayload(&facturador.InvoicePayload{}, nil)
}
