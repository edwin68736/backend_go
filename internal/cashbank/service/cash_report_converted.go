package service

import (
	"tukifac/internal/sales/nvdisplay"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// Comprobantes emitidos desde una nota de venta (NV) en los reportes de caja.
//
// El dinero entra una sola vez: en el pago de la NV, en la caja donde se vendió. Al convertir la NV en factura o boleta, el comprobante
// copia ese pago como línea INFORMATIVA (sin caja propia) y hereda la caja de la NV. Si el reporte contara también esa copia, el efectivo
// saldría dos veces en un rango de fechas, y en el reporte por caja el comprobante no aparecería (su pago no tiene caja).
//
// convertedChildPaymentExclusion deja fuera las líneas de pago SIN caja de un comprobante emitido desde una NV (las copias informativas;
// el dinero de la NV ya cuenta por su pago o, en datos históricos sin líneas de pago, por la venta de la NV). Un cobro posterior REAL del
// comprobante (a crédito, cobrado en una caja) sí tiene caja y se mantiene.
const convertedChildPaymentExclusion = `NOT (tenant_sales.issued_from_nota_sale_id IS NOT NULL AND tenant_sales.issued_from_nota_sale_id > 0
	AND tenant_sale_payments.cash_session_id IS NULL)`

// cashReportDisplayNumbers número a mostrar por venta en los reportes de caja. Igual que nvdisplay.LoadDisplayNumbersBySaleID, pero una NV
// ya convertida se rotula «F001-00000097 (desde NV001-00003492)»: sale el número del comprobante vigente y queda a la vista de qué nota
// de venta viene el dinero, para que quien busca el comprobante lo encuentre y no se confunda con la nota.
func cashReportDisplayNumbers(db *gorm.DB, saleIDs []uint) map[uint]string {
	out := nvdisplay.LoadDisplayNumbersBySaleID(db, saleIDs)
	issues := nvdisplay.LoadIssuesForParents(db, saleIDs)
	if len(issues) == 0 {
		return out
	}
	parentIDs := make([]uint, 0, len(issues))
	for id := range issues {
		parentIDs = append(parentIDs, id)
	}
	var parents []database.TenantSale
	if err := db.Select("id", "series", "number").Where("id IN ?", parentIDs).Find(&parents).Error; err != nil {
		return out
	}
	for _, nv := range parents {
		if shown := out[nv.ID]; shown != "" {
			out[nv.ID] = shown + " (desde " + nvdisplay.FormatDocumentNumber(nv.Series, nv.Number) + ")"
		}
	}
	return out
}
