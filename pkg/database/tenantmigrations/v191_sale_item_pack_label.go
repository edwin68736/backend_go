package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v191SaleItem struct {
	PackLabel string `gorm:"column:pack_label;size:60"`
}

func (v191SaleItem) TableName() string { return "tenant_sale_items" }

type v191QuotationItem struct {
	PackLabel string `gorm:"column:pack_label;size:60"`
}

func (v191QuotationItem) TableName() string { return "tenant_quotation_items" }

// V191SaleItemPackLabel agrega tenant_sale_items.pack_label y tenant_quotation_items.pack_label: el contenido de la unidad de venta
// («12 UND» para una caja de 12) que el comprobante imprime entre paréntesis junto a la descripción. Solo agrega columnas
// nullable: los documentos existentes NO se tocan ni se reconstruyen (quedan sin paréntesis, como hasta hoy). Idempotente.
type V191SaleItemPackLabel struct{}

func (V191SaleItemPackLabel) Version() int { return 191 }
func (V191SaleItemPackLabel) Name() string { return "sale_item_pack_label" }

func (V191SaleItemPackLabel) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if sale := &(v191SaleItem{}); !mig.HasColumn(sale, "PackLabel") {
		if err := mig.AddColumn(sale, "PackLabel"); err != nil {
			return fmt.Errorf("add tenant_sale_items.pack_label: %w", err)
		}
	}
	if quote := &(v191QuotationItem{}); !mig.HasColumn(quote, "PackLabel") {
		if err := mig.AddColumn(quote, "PackLabel"); err != nil {
			return fmt.Errorf("add tenant_quotation_items.pack_label: %w", err)
		}
	}
	return nil
}
