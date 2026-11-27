package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)

// Bug reportado (tenant 20615174638, guía T001-5, 20/11/2026): ver o descargar el PDF de una guía de
// remisión daba 500. GET /api/sales/:id arma print_data y enrichDespatchPrintData leía
// payload.Tercero.Address.Direccion; el tercero (remitente real) solo llevaba RUC y razón social, así que
// Address era nil y el panic quedaba capturado por recover como "error interno del servidor".
func TestBuildPrintDataForSale_guiaConPartesSinDireccion(t *testing.T) {
	const withTerceroNoAddress = `{"serie":"T001","correlativo":"5","tipoDoc":"09","version":"2022",
"company":{"ruc":"20000000001","razonSocial":"ACME SAC"},
"destinatario":{"tipoDoc":"6","numDoc":"20000000001","rznSocial":"ACME SAC","address":{"direccion":"Av. Central 100","ubigueo":"040129","codigoPais":"PE"}},
"tercero":{"tipoDoc":"6","numDoc":"20111111111","rznSocial":"TERCERO SA"},
"envio":{"codTraslado":"02","desTraslado":"Compra","modTraslado":"02","fecTraslado":"2026-11-20T08:24:17-05:00","numBultos":68,"pesoTotal":918,"undPesoTotal":"KGM",
"partida":{"direccion":"Av. Metropolitana 63","ubigueo":"040126","codigoPais":"PE"},"llegada":{"direccion":"Mz. I Lt. 4D","ubigueo":"040129","codigoPais":"PE"}},
"details":[{"cantidad":68,"codigo":"BP001","descripcion":"PILSEN 630MLX12","unidad":"BX"}]}`
	const destinatarioNoAddress = `{"serie":"T001","correlativo":"6","tipoDoc":"09","version":"2022",
"company":{"ruc":"20000000001","razonSocial":"ACME SAC"},
"destinatario":{"tipoDoc":"6","numDoc":"20222222222","rznSocial":"SIN DIRECCION SA"},
"envio":{"codTraslado":"01","desTraslado":"Venta","modTraslado":"02","fecTraslado":"2026-11-20T08:24:17-05:00","numBultos":1,"pesoTotal":1,"undPesoTotal":"KGM"},
"details":[{"cantidad":1,"codigo":"X","descripcion":"ITEM","unidad":"NIU"}]}`

	cases := []struct {
		name, payload, wantTercero string
	}{
		{"tercero sin dirección", withTerceroNoAddress, "TERCERO SA"},
		{"destinatario sin dirección", destinatarioNoAddress, ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, contact, branch := setupSnapshotDB(t)
			if err := db.AutoMigrate(&database.TenantDespatch{}, &database.TenantSale{}, &database.TenantSaleItem{}); err != nil {
				t.Fatal(err)
			}
			saleID := uint(1604 + i)
			cid := contact.ID
			sale := &database.TenantSale{
				ID: saleID, BranchID: branch.ID, ContactID: &cid, DocType: "GUIA_REMISION", Series: "T001", Number: "T001-0000000" + string(rune('5'+i)),
				IssueDate: time.Date(2026, 11, 20, 13, 24, 17, 0, time.UTC), Currency: "PEN", Status: "paid", BillingStatus: "accepted", OperationTypeCode: "0101",
			}
			if err := db.Create(sale).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&database.TenantSaleItem{SaleID: saleID, Description: "ITEM", Quantity: 1}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&database.TenantDespatch{SaleID: &saleID, PayloadJSON: tc.payload}).Error; err != nil {
				t.Fatal(err)
			}

			pd, err := BuildPrintDataForSale(db, saleID) // antes: panic nil pointer dereference
			if err != nil {
				t.Fatal(err)
			}
			if pd.Despatch == nil {
				t.Fatal("la guía debe traer los datos de traslado")
			}
			if tc.wantTercero != "" {
				if pd.Despatch.Transportista == nil || pd.Despatch.Transportista.Name != tc.wantTercero || pd.Despatch.Transportista.Address != "" {
					t.Fatalf("tercero mal armado: %+v", pd.Despatch.Transportista)
				}
			} else if pd.Despatch.Destinatario.Address != "" {
				t.Fatalf("dirección esperada vacía, llegó %q", pd.Despatch.Destinatario.Address)
			}
		})
	}
}

// Guía transportista cuyo destinatario es la propia empresa: no hay contacto en el directorio, pero
// la impresión debe mostrar a la empresa y no «Cliente genérico».
func TestBuildPrintDataForSale_guiaALaPropiaEmpresa(t *testing.T) {
	const payload = `{"serie":"V001","correlativo":"1","tipoDoc":"31","version":"2022",
"company":{"ruc":"20000000001","razonSocial":"TRANSPORTES SAC"},
"destinatario":{"tipoDoc":"6","numDoc":"20000000001","rznSocial":"TRANSPORTES SAC","address":{"direccion":"Av. Central 100","ubigueo":"150101","codigoPais":"PE"}},
"tercero":{"tipoDoc":"6","numDoc":"20000000001","rznSocial":"TRANSPORTES SAC"},
"envio":{"codTraslado":"01","desTraslado":"Venta","modTraslado":"02","fecTraslado":"2026-11-27T08:00:00-05:00","numBultos":1,"pesoTotal":1,"undPesoTotal":"KGM"},
"details":[{"cantidad":1,"codigo":"X","descripcion":"ITEM","unidad":"NIU"}]}`
	db, _, branch := setupSnapshotDB(t)
	if err := db.AutoMigrate(&database.TenantDespatch{}, &database.TenantSale{}, &database.TenantSaleItem{}); err != nil {
		t.Fatal(err)
	}
	saleID := uint(1700)
	sale := &database.TenantSale{
		ID: saleID, BranchID: branch.ID, DocType: "GUIA_TRANSPORTISTA", Series: "V001", Number: "V001-00000001",
		IssueDate: time.Date(2026, 11, 27, 13, 0, 0, 0, time.UTC), Currency: "PEN", Status: "paid", BillingStatus: "pending",
	}
	if err := db.Create(sale).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.TenantSaleItem{SaleID: saleID, Description: "ITEM", Quantity: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.TenantDespatch{SaleID: &saleID, PayloadJSON: payload}).Error; err != nil {
		t.Fatal(err)
	}
	pd, err := BuildPrintDataForSale(db, saleID)
	if err != nil {
		t.Fatal(err)
	}
	if pd.Client == nil || pd.Client.DocNumber != "20000000001" || pd.Client.BusinessName != "TRANSPORTES SAC" {
		t.Fatalf("la impresión debe mostrar al destinatario de la guía: %+v", pd.Client)
	}
}
