package service

import (
	"fmt"
	"strconv"
	"strings"

	"tukifac/pkg/database"
	"tukifac/pkg/saas/docusage"
)

func parseCorrelativeUint(s string) uint {
	n, _ := strconv.ParseUint(s, 10, 32)
	return uint(n)
}

func (s *BillingService) effectiveTenantID(explicit uint) uint {
	if explicit > 0 {
		return explicit
	}
	return s.centralTenantID
}

// reserveSaleDocument consume cupo antes de emitir a SUNAT (idempotente por sale_id).
func (s *BillingService) reserveSaleDocument(tenantID, saleID uint) error {
	tenantID = s.effectiveTenantID(tenantID)
	if tenantID == 0 {
		return docusage.ErrTenantRequired
	}
	var sale database.TenantSale
	if err := s.db.First(&sale, saleID).Error; err != nil {
		return err
	}
	var ser database.TenantDocumentSeries
	sunatCode := ""
	if err := s.db.First(&ser, sale.SeriesID).Error; err == nil {
		sunatCode = ser.SunatCode
	}
	if !docusage.IsCountableSunatCode(sunatCode) {
		return nil
	}
	docType := docusage.SunatCodeToDocType(sunatCode)
	docNum := fmt.Sprintf("%s-%s-%s", ser.Series, ser.SunatCode, sale.Number)
	return docusage.ReserveElectronicDocument(docusage.ReserveInput{
		TenantID:       tenantID,
		DocumentType:   docType,
		DocumentID:     saleID,
		DocumentNumber: docNum,
		Source:         "async",
	})
}

func (s *BillingService) reserveGenericDocument(docType string, docID uint, docNumber string) error {
	tenantID := s.centralTenantID
	if tenantID == 0 {
		return docusage.ErrTenantRequired
	}
	if docID == 0 {
		return nil
	}
	return docusage.ReserveElectronicDocument(docusage.ReserveInput{
		TenantID: tenantID, DocumentType: docType, DocumentID: docID,
		DocumentNumber: docNumber, Source: "sync",
	})
}

// TenantIDFromDB resuelve tenant central por nombre BD (worker/cola).
func TenantIDFromDB(tenantDBName string) uint {
	if tenantDBName == "" || database.CentralDB == nil {
		return 0
	}
	var t database.Tenant
	if database.CentralDB.Where("db_name = ?", tenantDBName).First(&t).Error == nil {
		return t.ID
	}
	return 0
}

// TenantSlugFromID resuelve slug SaaS por id central (worker/cola).
func TenantSlugFromID(tenantID uint) string {
	if tenantID == 0 || database.CentralDB == nil {
		return ""
	}
	var t database.Tenant
	if database.CentralDB.Select("slug").First(&t, tenantID).Error == nil {
		return strings.TrimSpace(t.Slug)
	}
	return ""
}

// TenantSlugFromDB resuelve slug SaaS por nombre BD tenant.
func TenantSlugFromDB(tenantDBName string) string {
	if tenantDBName == "" || database.CentralDB == nil {
		return ""
	}
	var t database.Tenant
	if database.CentralDB.Where("db_name = ?", tenantDBName).First(&t).Error == nil {
		return strings.TrimSpace(t.Slug)
	}
	return ""
}

// ResolveTenantSlug obtiene slug desde job o BD central.
func ResolveTenantSlug(tenantID uint, tenantDB, tenantSlug string) string {
	if s := strings.TrimSpace(tenantSlug); s != "" {
		return s
	}
	if s := TenantSlugFromID(tenantID); s != "" {
		return s
	}
	return TenantSlugFromDB(tenantDB)
}
