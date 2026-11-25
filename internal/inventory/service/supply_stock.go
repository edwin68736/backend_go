package service

// F3a: carga inicial e ingreso/merma de insumos, protegidos contra repeticiones.

import (
	"errors"
	"strings"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// ErrSupplyAlreadyLoaded: la carga inicial es solo para un insumo sin historia en esa sucursal.
var ErrSupplyAlreadyLoaded = errors.New("este insumo ya tiene movimientos en la sucursal: usa ingreso o merma")

// RecordSupplyInitialStock registra las existencias de partida con su costo por unidad base. Repetirla (o hacerla dos veces a la
// vez) no duplica: se bloquea el saldo y se exige que no haya ningún movimiento.
func (s *InventoryService) RecordSupplyInitialStock(productID, branchID uint, quantity, unitCost float64, userID uint) error {
	if quantity <= 0 {
		return errors.New("la cantidad debe ser mayor a cero")
	}
	if unitCost < 0 {
		return errors.New("el costo no puede ser negativo")
	}
	return runInventoryTx(s.db, func(tx *gorm.DB) error {
		if _, _, err := acquireStockRow(tx, stockTarget{entityID: productID, branchID: branchID}); err != nil {
			return err
		}
		var ids []uint
		q := tx.Model(&database.TenantStockMovement{}).Select("id").Where("product_id = ? AND branch_id = ?", productID, branchID).Limit(1)
		if err := currentRead(tx, q).Find(&ids).Error; err != nil {
			return err
		}
		if len(ids) > 0 {
			return ErrSupplyAlreadyLoaded
		}
		return NewInventoryService(tx).RecordMovementTx(tx, MovementInput{
			ProductID: productID, BranchID: branchID, Type: "in", Quantity: quantity, UnitCost: unitCost,
			Reference: "STOCK_INICIAL", Notes: "Carga inicial de insumos", UserID: userID, OperationCode: "INITIAL_STOCK",
		})
	})
}

// SupplyEntryInput: ingreso sin comprobante (compra al paso) o salida por merma/desperdicio, en unidad base.
type SupplyEntryInput struct {
	ProductID uint
	BranchID  uint
	UserID    uint
	Direction string // IN | OUT
	Operation string // código del tipo de operación
	Quantity  float64
	UnitCost  float64 // solo ingresos
	Notes     string
	Key       string // clave del intento: repetirla no crea otro documento
}

const supplyEntryReferencePrefix = "APP:"

// RecordSupplyEntry crea y confirma el documento de inventario en una sola transacción. replay=true si la clave ya lo había creado.
func (s *InventoryDocumentService) RecordSupplyEntry(in SupplyEntryInput) (docID uint, replay bool, err error) {
	key := strings.TrimSpace(in.Key)
	if key == "" || len(key) > 60 {
		return 0, false, errors.New("falta la clave del intento")
	}
	var op database.TenantInventoryOperationType
	if err := s.db.Where("code = ?", in.Operation).First(&op).Error; err != nil {
		return 0, false, errors.New("tipo de operación no disponible")
	}
	reference := supplyEntryReferencePrefix + key
	err = runInventoryTx(s.db, func(tx *gorm.DB) error {
		docID, replay = 0, false
		// La sucursal se bloquea primero: así su serie de inventario (si falta) se crea una sola vez. Crearla dos veces la dejaría
		// con series repetidas y sin poder registrar ingresos.
		var branches []uint
		bq := tx.Model(&database.TenantBranch{}).Select("id").Where("id = ?", in.BranchID)
		if err := currentRead(tx, bq).Find(&branches).Error; err != nil {
			return err
		}
		if err := database.SeedInventoryDocumentSeriesForBranch(tx, in.BranchID); err != nil {
			return err
		}
		// El saldo bloqueado serializa dos envíos simultáneos del mismo intento.
		if _, _, err := acquireStockRow(tx, stockTarget{entityID: in.ProductID, branchID: in.BranchID}); err != nil {
			return err
		}
		var existing []uint
		q := tx.Model(&database.TenantInventoryDocument{}).Select("id").
			Where("branch_id = ? AND reference = ?", in.BranchID, reference).Limit(1)
		if err := currentRead(tx, q).Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			docID, replay = existing[0], true
			return nil
		}
		docs := &InventoryDocumentService{db: tx}
		id, err := docs.createInventoryDocumentTx(tx, CreateDocumentInput{
			Direction: in.Direction, OperationTypeID: op.ID, BranchID: in.BranchID, DocumentDate: time.Now(),
			Reference: reference, MovementReason: strings.TrimSpace(in.Notes), UserID: in.UserID, Manual: true, Source: DocumentSourceManual,
			Lines: []DocumentLineInput{{ProductID: in.ProductID, Quantity: in.Quantity, UnitCost: in.UnitCost}},
		})
		if err != nil {
			return err
		}
		docID = id
		return docs.confirmInventoryDocumentTx(tx, id, in.UserID)
	})
	return docID, replay, err
}
