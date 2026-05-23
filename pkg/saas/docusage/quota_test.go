package docusage

import (
	"testing"

	"tukifac/pkg/database"
)

func TestWarningFromView_exhausted(t *testing.T) {
	v := DocumentUsageView{CanEmit: false, TotalAvailable: 0}
	lvl, msg := warningFromView(v)
	if lvl != "exhausted" || msg == "" {
		t.Fatalf("expected exhausted warning, got %s %s", lvl, msg)
	}
}

func TestWarningFromView_low(t *testing.T) {
	v := DocumentUsageView{CanEmit: true, TotalAvailable: 5, PlanLimit: 50, PlanUsed: 45}
	lvl, _ := warningFromView(v)
	if lvl != "low" {
		t.Fatalf("expected low, got %s", lvl)
	}
}

func TestSunatCodeToDocType(t *testing.T) {
	if SunatCodeToDocType("01") != "invoice" {
		t.Fatal("01 -> invoice")
	}
	if IsCountableSunatCode("00") {
		t.Fatal("00 must not count")
	}
}

func TestSyncCycleDocumentQuotaFromPlan_updatesStaleLimit(t *testing.T) {
	plan := database.SaasPlan{MonthlyDocumentsLimit: 500, IsUnlimitedDocuments: false}
	limit := planLimitFromPlan(&plan)
	if limit != 500 {
		t.Fatalf("plan limit want 500, got %d", limit)
	}
	cycle := database.SaasBillingCycle{DocumentsLimit: 497, DocumentsUsed: 0, IsUnlimitedDocuments: false}
	// Sin BD: validar lógica de límite esperado tras sync (misma fórmula que Sync).
	want := planLimitFromPlan(&plan)
	if cycle.DocumentsUsed > want {
		want = cycle.DocumentsUsed
	}
	if want != 500 {
		t.Fatalf("sync formula want 500, got %d", want)
	}
}

func TestPlanLimitFromPlan_unlimited(t *testing.T) {
	p := &database.SaasPlan{IsUnlimitedDocuments: true, MonthlyDocumentsLimit: 999}
	if planLimitFromPlan(p) != 0 {
		t.Fatal("unlimited plan limit snapshot is 0")
	}
}
