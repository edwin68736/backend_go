package tenantbackfills

import "testing"

func TestIsRepeatable_V036IsRepeatable(t *testing.T) {
	if !IsRepeatable(V036SalePaymentCashSessionBackfill{}) {
		t.Fatal("V036SalePaymentCashSessionBackfill debe ser Repeatable: depende de schema " +
			"(V123, tenant_cash_movements/tenant_bank_movements) desplegado de forma " +
			"incremental por tenant; el candado run-once genérico puede trabarlo con un " +
			"resultado parcial para siempre (bug encontrado en producción 2026-09-06)")
	}
}

func TestIsRepeatable_DefaultFalseForOtherBackfills(t *testing.T) {
	for _, bf := range []TenantBackfill{
		V031BranchBackfill{},
		V032RestaurantOrdersBackfill{},
		V033FinalizeOrphanTableOrders{},
		V034ProductCodes{},
		V035MergeDuplicateContacts{},
	} {
		if IsRepeatable(bf) {
			t.Fatalf("%s no debería ser Repeatable (candado run-once sigue siendo correcto para "+
				"este backfill; solo V036 se eximió explícitamente)", bf.Name())
		}
	}
}
