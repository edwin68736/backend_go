package service

import (
	"testing"

	"tukifac/pkg/database"
)

// Observaciones del panel de equipos del 19-nov (ver docs/equipos/ANALISIS_OBS_19-11.md).

func officeInput(f *fixture, mode string) OrderInput {
	in := f.input(1, 100)
	in.Shipment = &ShipmentInput{DeliveryMode: mode}
	return in
}

func TestOrder_officeSaleIsDeliveredInOffice(t *testing.T) {
	f := newFixture(t)
	r, err := f.s.CreateOrder(officeInput(f, "oficina"), 1)
	if err != nil {
		t.Fatal(err)
	}
	sh := r.Order.Shipment
	if sh == nil || sh.Status != "entregado" || sh.PickedUpAt == nil || sh.DeliveryMode != "oficina" {
		t.Fatalf("un pedido de oficina nace entregado: %+v", sh)
	}
	// Agencia: sigue por despachar.
	r2, err := f.s.CreateOrder(f.input(1, 100), 1)
	if err != nil || r2.Order.Shipment.Status != "pendiente_envio" {
		t.Fatalf("agencia nace por despachar: %v %+v", err, r2.Order.Shipment)
	}
	// El valor antiguo pendiente_recojo conserva el flujo manual.
	r3, err := f.s.CreateOrder(officeInput(f, "pendiente_recojo"), 1)
	if err != nil || r3.Order.Shipment.Status != "pendiente_envio" {
		t.Fatalf("pendiente_recojo no cambia: %v %+v", err, r3.Order.Shipment)
	}
}

func TestOrder_switchingModeAfterOfficeDelivery(t *testing.T) {
	f := newFixture(t)
	r, _ := f.s.CreateOrder(f.input(1, 100), 1)
	// agencia → oficina: se entrega en el acto
	in := officeInput(f, "oficina")
	if _, err := f.s.UpdateOrder(r.Order.ID, in, ConfirmInput{}, 1); err != nil {
		t.Fatal(err)
	}
	o, _ := f.s.GetOrder(r.Order.ID)
	if o.Shipment.Status != "entregado" {
		t.Fatalf("agencia→oficina: %s", o.Shipment.Status)
	}
	// oficina → agencia: vuelve a por despachar
	if _, err := f.s.UpdateOrder(r.Order.ID, f.input(1, 100), ConfirmInput{}, 1); err != nil {
		t.Fatal(err)
	}
	o, _ = f.s.GetOrder(r.Order.ID)
	if o.Shipment.Status != "pendiente_envio" || o.Shipment.PickedUpAt != nil {
		t.Fatalf("oficina→agencia: %+v", o.Shipment)
	}
}

func TestListOrders_shipmentFilterSplitsOfficeFromPickedUp(t *testing.T) {
	f := newFixture(t)
	office, _ := f.s.CreateOrder(officeInput(f, "oficina"), 1)
	agency := f.shipped(t, 1, 100)
	if _, err := f.s.MarkPickedUp(agency.ID, ""); err != nil {
		t.Fatal(err)
	}
	ids := func(status string) []uint {
		res, err := f.s.ListOrders(OrderFilter{ShipmentStatus: status})
		if err != nil {
			t.Fatal(err)
		}
		var out []uint
		for _, r := range res.Rows {
			out = append(out, r.ID)
		}
		return out
	}
	if got := ids("entregado_oficina"); len(got) != 1 || got[0] != office.Order.ID {
		t.Errorf("entregado_oficina: %v", got)
	}
	if got := ids("entregado"); len(got) != 1 || got[0] != agency.ID {
		t.Errorf("recogido (agencia) no incluye oficina: %v", got)
	}
}

func TestListOrders_summaryItemsOnePerProduct(t *testing.T) {
	f := newFixture(t)
	in := f.input(1, 100)
	rid := f.roll.ID
	in.Items = append(in.Items, OrderItemInput{LineType: "producto", ProductID: &rid, Quantity: 5, UnitPrice: 10})
	r, err := f.s.CreateOrder(in, 1)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := f.s.ListOrders(OrderFilter{})
	var row *OrderRow
	for i := range res.Rows {
		if res.Rows[i].ID == r.Order.ID {
			row = &res.Rows[i]
		}
	}
	if row == nil || len(row.SummaryItems) != 2 || row.SummaryItems[0] != "1x TK-E583" || row.SummaryItems[1] != "5x ROLL" {
		t.Fatalf("summary_items: %+v", row)
	}
}

func TestStock_damagedColumnFromReceivedDamagedReturns(t *testing.T) {
	f := newFixture(t)
	r := openReturn(t, f)
	if _, err := f.s.ReceiveReturnWith(r.ID, ReceiveInput{Condition: "danado", Notes: "pantalla rota"}, 1); err != nil {
		t.Fatal(err)
	}
	r2 := openReturn(t, f) // en buen estado: no cuenta como dañado
	if _, err := f.s.ReceiveReturn(r2.ID, "buen_estado", 1); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.StockReport(CurrentPeriod())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ProductID == f.printer.ID && row.Damaged != 2 {
			t.Errorf("dañados de la impresora = %d, quiero 2", row.Damaged)
		}
		if row.ProductID == f.roll.ID && row.Damaged != 0 {
			t.Errorf("sin dañados en rollos: %d", row.Damaged)
		}
	}
}

func TestReceiveReturnWith_costNotesAndDiscarded(t *testing.T) {
	f := newFixture(t)
	r := openReturn(t, f)
	cost, unpaid := 12.5, 40.0
	v, err := f.s.ReceiveReturnWith(r.ID, ReceiveInput{Condition: "buen_estado", ReturnCost: &cost, UnpaidBalance: &unpaid, Notes: "llegó completo"}, 1)
	if err != nil || v.Status != "recibido" || v.ReturnCost != 12.5 || v.UnpaidBalance != 40 || v.Notes != "llegó completo" {
		t.Fatalf("recibir con datos: %v %+v", err, v)
	}
	r2 := openReturn(t, f)
	before := f.stock(t, f.printer.ID)
	v2, err := f.s.ReceiveReturnWith(r2.ID, ReceiveInput{Condition: "danado", Discarded: true}, 1)
	if err != nil || v2.Status != "desechado_por_agencia" {
		t.Fatalf("desechado: %v %+v", err, v2)
	}
	if f.stock(t, f.printer.ID) != before {
		t.Error("desechado no reingresa")
	}
}

func TestListPlans_twelveFixedOptionsWithCyclePrice(t *testing.T) {
	f := newFixture(t)
	if err := f.s.db.AutoMigrate(&database.SaasPlan{}, &database.SaasPlanCycle{}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []database.SaasPlan{
		{Name: "MICROEMPRESA", Price: 29.5, Active: true}, {Name: "MICROEMPRESA PRO", Price: 40, Active: true},
		{Name: "Emprendedor", Price: 50, Active: true}, {Name: "PREMIUM", Price: 100, Active: true}, {Name: "CORPORATIVO", Price: 200, Active: true},
		{Name: "Trial", Price: 0, Active: true}, {Name: "ILIMITADO", Price: 300, Active: false},
	} {
		p := p
		if err := f.s.db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
	}
	var micro database.SaasPlan
	f.s.db.Where("name = ?", "MICROEMPRESA").First(&micro)
	f.s.db.Create(&database.SaasPlanCycle{PlanID: micro.ID, Months: 12, DiscountType: "percent", DiscountValue: 10, Enabled: true})
	got, err := f.s.ListPlans()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 {
		t.Fatalf("12 opciones, hay %d: %+v", len(got), got)
	}
	if got[0].Name != "Microempresa plan mensual" || got[0].Months != 1 || got[0].Price != 29.5 {
		t.Errorf("mensual: %+v", got[0])
	}
	if got[1].Name != "Microempresa plan semestral" || got[1].Months != 6 || got[1].Price != 177 {
		t.Errorf("semestral: %+v", got[1])
	}
	if got[2].Name != "Microempresa plan anual" || got[2].Months != 12 || got[2].Price != 318.6 {
		t.Errorf("anual con 10%% de descuento: %+v", got[2])
	}
	if got[11].Name != "Corporativo plan anual" {
		t.Errorf("última: %+v", got[11])
	}
}
