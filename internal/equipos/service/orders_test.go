package service

import (
	"errors"
	"strings"
	"testing"

	"tukifac/pkg/database"
)

type fixture struct {
	s       *Service
	printer *database.EquipProduct
	roll    *database.EquipProduct
	combo   *ComboView
	carrier *database.EquipCarrier
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s := New(setupEquiposDB(t))
	f := &fixture{s: s}
	f.printer = mustProduct(t, s, "TK-E583", 0, 0)
	f.roll = mustProduct(t, s, "ROLL", 0, 0)
	for _, p := range []*database.EquipProduct{f.printer, f.roll} {
		if _, err := s.UpdateProduct(p.ID, ProductInput{Code: p.Code, Name: p.Name, Kind: "equipo", ReferencePrice: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddMovement(MovementInput{ProductID: f.printer.ID, MovementType: "ingreso", Quantity: 10, Note: "inicial"}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMovement(MovementInput{ProductID: f.roll.ID, MovementType: "ingreso", Quantity: 100, Note: "inicial"}, 1); err != nil {
		t.Fatal(err)
	}
	var err error
	f.combo, err = s.CreateCombo(ComboInput{Code: "KIT", Items: []ComboItemInput{{ProductID: f.printer.ID, Quantity: 1}, {ProductID: f.roll.ID, Quantity: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	f.carrier, err = s.CreateCarrier(CarrierInput{Code: "shalom", Name: "Shalom", DispatchDays: "0,1,2,3,4,5,6", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) stock(t *testing.T, id uint) int {
	t.Helper()
	var n int
	if err := f.s.db.Model(&database.EquipStockMovement{}).Where("product_id = ?", id).Select("COALESCE(SUM(quantity),0)").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) input(qty float64, price float64) OrderInput {
	pid := f.printer.ID
	return OrderInput{
		CustomerName: "Juan Pérez", CustomerDocType: "DNI", CustomerDocNumber: "12345678", CustomerPhone: "987654321", SaveCustomer: true,
		SaleType: "independiente",
		Items:    []OrderItemInput{{LineType: "producto", ProductID: &pid, Quantity: qty, UnitPrice: price}},
		Shipment: &ShipmentInput{CarrierID: &f.carrier.ID, DestinationDepartment: "Lima", DeliveryMode: "agencia"},
	}
}

func (f *fixture) confirmed(t *testing.T, qty, price float64) *OrderView {
	t.Helper()
	r, err := f.s.CreateOrder(f.input(qty, price), 1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.s.ConfirmOrder(r.Order.ID, ConfirmInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return c.Order
}

func TestOrder_createConfirmDeductsStockAndNumbers(t *testing.T) {
	f := newFixture(t)
	r, err := f.s.CreateOrder(f.input(2, 150), 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Order.Status != "borrador" || r.Order.OrderNumber != 1 || r.Order.TotalAmount != 300 || r.Order.BalanceAmount != 300 {
		t.Fatalf("pedido creado: %+v", r.Order.EquipOrder)
	}
	if f.stock(t, f.printer.ID) != 10 {
		t.Fatal("un borrador no descuenta stock")
	}
	c, err := f.s.ConfirmOrder(r.Order.ID, ConfirmInput{}, 1)
	if err != nil || c.Order.Status != "registrado" {
		t.Fatalf("confirmar: %v", err)
	}
	if got := f.stock(t, f.printer.ID); got != 8 {
		t.Fatalf("stock tras confirmar = %d, quiero 8", got)
	}
	r2, _ := f.s.CreateOrder(f.input(1, 100), 1)
	if r2.Order.OrderNumber != 2 {
		t.Errorf("numeración continua: %d", r2.Order.OrderNumber)
	}
	if _, err := f.s.ConfirmOrder(r.Order.ID, ConfirmInput{}, 1); err == nil {
		t.Error("confirmar dos veces debe fallar")
	}
}

func TestOrder_comboExpandsComponents(t *testing.T) {
	f := newFixture(t)
	in := f.input(1, 0)
	in.Items = []OrderItemInput{{LineType: "combo", ComboID: &f.combo.ID, Quantity: 2, UnitPrice: 400}}
	r, err := f.s.CreateOrder(in, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ConfirmOrder(r.Order.ID, ConfirmInput{}, 1); err != nil {
		t.Fatal(err)
	}
	if f.stock(t, f.printer.ID) != 8 || f.stock(t, f.roll.ID) != 80 {
		t.Fatalf("combo debe descontar componentes: %d %d", f.stock(t, f.printer.ID), f.stock(t, f.roll.ID))
	}
	// Cambiar la composición del combo después no altera el pedido ya vendido.
	if _, err := f.s.UpdateCombo(f.combo.ID, ComboInput{Code: "KIT", Items: []ComboItemInput{{ProductID: f.printer.ID, Quantity: 5}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CancelOrder(r.Order.ID, "prueba", 1); err != nil {
		t.Fatal(err)
	}
	if f.stock(t, f.printer.ID) != 10 || f.stock(t, f.roll.ID) != 100 {
		t.Fatal("anular devuelve exactamente lo descontado")
	}
}

func TestOrder_negativeStockGate(t *testing.T) {
	f := newFixture(t)
	r, err := f.s.CreateOrder(f.input(12, 100), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.ConfirmOrder(r.Order.ID, ConfirmInput{}, 1)
	var neg *NegativeStockError
	if !errors.As(err, &neg) || len(neg.Items) != 1 || neg.Items[0].Resulting != -2 {
		t.Fatalf("debe advertir stock negativo: %v", err)
	}
	if _, err := f.s.ConfirmOrder(r.Order.ID, ConfirmInput{AllowNegative: true}, 1); err == nil || !strings.Contains(err.Error(), "nota") {
		t.Errorf("confirmar en negativo exige nota: %v", err)
	}
	if _, err := f.s.ConfirmOrder(r.Order.ID, ConfirmInput{AllowNegative: true, NegativeNote: "llega reposición"}, 1); err != nil {
		t.Fatal(err)
	}
	if f.stock(t, f.printer.ID) != -2 {
		t.Fatal("stock negativo permitido con nota")
	}
}

func TestOrder_validationErrors(t *testing.T) {
	f := newFixture(t)
	pid := f.printer.ID
	bad := f.input(1, 100)
	bad.CustomerDocNumber = "123"
	if _, err := f.s.CreateOrder(bad, 1); err == nil || !strings.Contains(err.Error(), "DNI") {
		t.Errorf("DNI inválido: %v", err)
	}
	bad = f.input(1, 100)
	bad.Items = nil
	if r, err := f.s.CreateOrder(bad, 1); err != nil {
		t.Fatalf("un borrador sin ítems se permite: %v", err)
	} else if _, err := f.s.ConfirmOrder(r.Order.ID, ConfirmInput{}, 1); err == nil {
		t.Error("confirmar sin ítems debe fallar")
	}
	bad = f.input(0, 100)
	if _, err := f.s.CreateOrder(bad, 1); err == nil {
		t.Error("cantidad 0 debe fallar")
	}
	bad = f.input(1, -5)
	if _, err := f.s.CreateOrder(bad, 1); err == nil {
		t.Error("precio negativo debe fallar")
	}
	bad = f.input(1, 100)
	bad.SaleType = "regalado"
	if _, err := f.s.CreateOrder(bad, 1); err == nil {
		t.Error("tipo de venta inválido debe fallar")
	}
	// Obsequio: precio forzado a 0.
	ok := f.input(1, 100)
	ok.Items = append(ok.Items, OrderItemInput{LineType: "producto", ProductID: &pid, Quantity: 1, UnitPrice: 50, IsCourtesy: true})
	r, err := f.s.CreateOrder(ok, 1)
	if err != nil || r.Order.TotalAmount != 100 {
		t.Fatalf("obsequio no suma al total: %v %+v", err, r)
	}
	// Línea libre.
	free := f.input(1, 100)
	free.Items = []OrderItemInput{{LineType: "otro", Description: "Instalación", Quantity: 1, UnitPrice: 30}}
	if r, err := f.s.CreateOrder(free, 1); err != nil || r.Order.TotalAmount != 30 {
		t.Fatalf("línea libre: %v", err)
	}
}

func TestOrder_updateRegeneratesMovements(t *testing.T) {
	f := newFixture(t)
	o := f.confirmed(t, 2, 100)
	if f.stock(t, f.printer.ID) != 8 {
		t.Fatal("precondición")
	}
	r, err := f.s.UpdateOrder(o.ID, f.input(5, 100), ConfirmInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if f.stock(t, f.printer.ID) != 5 || r.Order.TotalAmount != 500 {
		t.Fatalf("editar cantidades ajusta stock: stock=%d total=%v", f.stock(t, f.printer.ID), r.Order.TotalAmount)
	}
	if _, err := f.s.UpdateOrder(o.ID, f.input(1, 100), ConfirmInput{}, 1); err != nil || f.stock(t, f.printer.ID) != 9 {
		t.Fatalf("bajar cantidad devuelve stock: %v %d", err, f.stock(t, f.printer.ID))
	}
}

func TestOrder_duplicateWarning(t *testing.T) {
	f := newFixture(t)
	f.confirmed(t, 1, 100)
	r, err := f.s.CreateOrder(f.input(1, 100), 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(strings.ToLower(w), "pedido") {
			found = true
		}
	}
	if !found {
		t.Errorf("debe avisar posible duplicado: %v", r.Warnings)
	}
}

func TestDispatchFlow_requiresValidationAndGuide(t *testing.T) {
	f := newFixture(t)
	o := f.confirmed(t, 1, 100)
	if _, err := f.s.DispatchOrder(o.ID, ""); err == nil || !strings.Contains(err.Error(), "valid") {
		t.Fatalf("sin validar no se despacha: %v", err)
	}
	if _, err := f.s.ValidateOrder(o.ID, "ok", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DispatchOrder(o.ID, ""); err == nil || !strings.Contains(err.Error(), "guía") {
		t.Fatalf("por agencia exige guía: %v", err)
	}
	if _, err := f.s.UpdateShipment(o.ID, ShipmentInput{CarrierID: &f.carrier.ID, GuideNumber: "ABC123", DestinationDepartment: "Cusco", DeliveryMode: "agencia"}); err != nil {
		t.Fatal(err)
	}
	d, err := f.s.DispatchOrder(o.ID, "")
	if err != nil || d.Order.Shipment.Status != "en_transito" {
		t.Fatalf("despachar: %v", err)
	}
	a, err := f.s.MarkArrived(o.ID, "")
	if err != nil || a.Order.Shipment.Status != "en_agencia" || a.Order.Shipment.PickupDeadline == nil {
		t.Fatalf("llegada fija el plazo de recojo: %v", err)
	}
	p, err := f.s.MarkPickedUp(o.ID, "")
	if err != nil || p.Order.Shipment.Status != "entregado" {
		t.Fatalf("recojo: %v", err)
	}
	if len(p.Warnings) == 0 {
		t.Error("recoger con saldo pendiente debe avisar")
	}
	if _, err := f.s.CancelOrder(o.ID, "x", 1); err == nil {
		t.Error("un pedido entregado no se anula")
	}
}

func TestShipment_observedBlocksDispatchAndQueue(t *testing.T) {
	f := newFixture(t)
	o := f.confirmed(t, 1, 100)
	if _, err := f.s.ObserveOrder(o.ID, "falta DNI", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DispatchOrder(o.ID, ""); err == nil {
		t.Error("observado no se despacha")
	}
	rows, err := f.s.ListShipments(ShipmentFilter{Status: "pendiente_envio"})
	if err != nil || len(rows) != 1 || rows[0].ReadyToDispatch {
		t.Fatalf("cola por despachar: %v %+v", err, rows)
	}
	if _, err := f.s.ValidateOrder(o.ID, "", 1); err != nil {
		t.Fatal(err)
	}
	rows, _ = f.s.ListShipments(ShipmentFilter{Status: "pendiente_envio"})
	if len(rows) != 1 || len(rows[0].Packing) == 0 {
		t.Fatalf("lista de empaque: %+v", rows)
	}
}

func TestPayments_partialFullAndVoid(t *testing.T) {
	f := newFixture(t)
	o := f.confirmed(t, 2, 100) // 200
	cid := *o.CustomerID
	pay, err := f.s.CreatePayment(PaymentInput{CustomerID: &cid, Amount: 80, Method: "yape", Moment: "anticipado", Reference: "OP1", AutoAllocate: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := f.s.GetOrder(o.ID)
	if v.PaidAmount != 80 || v.BalanceAmount != 120 || v.PaymentStatus != "parcial" {
		t.Fatalf("pago parcial: %+v", v.EquipOrder)
	}
	if _, err := f.s.CreatePayment(PaymentInput{CustomerID: &cid, Amount: 120, Method: "efectivo", Moment: "al_recoger", Allocations: []PaymentAllocationInput{{OrderID: o.ID, Amount: 120}}}, 1); err != nil {
		t.Fatal(err)
	}
	v, _ = f.s.GetOrder(o.ID)
	if v.BalanceAmount != 0 || v.PaymentStatus != "pagado" {
		t.Fatalf("pago total: %+v", v.EquipOrder)
	}
	if _, err := f.s.VoidPayment(pay.ID, "error de monto"); err != nil {
		t.Fatal(err)
	}
	v, _ = f.s.GetOrder(o.ID)
	if v.PaidAmount != 120 || v.PaymentStatus != "parcial" {
		t.Fatalf("anular cobro recalcula: %+v", v.EquipOrder)
	}
	if _, err := f.s.CancelOrder(o.ID, "x", 1); err == nil {
		t.Error("con cobros vigentes no se anula el pedido")
	}
}

func TestPayments_overpayCreditAndInvoiceDuplicate(t *testing.T) {
	f := newFixture(t)
	o := f.confirmed(t, 1, 100)
	cid := *o.CustomerID
	if _, err := f.s.CreatePayment(PaymentInput{CustomerID: &cid, Amount: 150, Method: "yape", Moment: "anticipado", Allocations: []PaymentAllocationInput{{OrderID: o.ID, Amount: 150}}}, 1); err == nil {
		t.Error("aplicar más que el saldo debe fallar")
	}
	p, err := f.s.CreatePayment(PaymentInput{CustomerID: &cid, Amount: 150, Method: "yape", Moment: "anticipado", Invoice: "F002-59", AutoAllocate: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnallocatedAmount != 50 {
		t.Fatalf("el excedente queda como saldo a favor: %+v", p.EquipPayment)
	}
	acc, err := f.s.CustomerAccount(cid)
	if err != nil || acc.Credit != 50 || acc.TotalPaid != 150 {
		t.Fatalf("estado de cuenta: %+v %v", acc, err)
	}
	if _, err := f.s.CreatePayment(PaymentInput{CustomerID: &cid, Amount: 10, Method: "yape", Moment: "anticipado", Invoice: "f002-59"}, 1); err == nil || !strings.Contains(err.Error(), "F002-59") {
		t.Errorf("comprobante duplicado: %v", err)
	}
	if _, err := f.s.CreatePayment(PaymentInput{CustomerID: &cid, Amount: 10, Method: "yape", Moment: "anticipado", Invoice: "mal"}, 1); err == nil {
		t.Error("formato de comprobante inválido")
	}
	if _, err := f.s.CreatePayment(PaymentInput{CustomerID: &cid, Amount: 0, Method: "yape", Moment: "anticipado"}, 1); err == nil {
		t.Error("monto 0 inválido")
	}
	// Un segundo pedido consume el saldo a favor.
	o2 := f.confirmed(t, 1, 40)
	if _, err := f.s.AllocateCredit(p.ID, []PaymentAllocationInput{{OrderID: o2.ID, Amount: 40}}); err != nil {
		t.Fatal(err)
	}
	v, _ := f.s.GetOrder(o2.ID)
	if v.PaymentStatus != "pagado" {
		t.Fatalf("saldo a favor aplicado: %+v", v.EquipOrder)
	}
	bal, err := f.s.OpenBalances(cid)
	if err != nil || len(bal) != 0 {
		t.Fatalf("sin saldos abiertos: %v %+v", err, bal)
	}
}

func TestSetNoPaymentAndListFilters(t *testing.T) {
	f := newFixture(t)
	o := f.confirmed(t, 1, 100)
	v, err := f.s.SetNoPayment(o.ID, true)
	if err != nil || v.PaymentStatus != "no_pago" {
		t.Fatalf("no_pago: %v %+v", err, v)
	}
	res, err := f.s.ListOrders(OrderFilter{PaymentStatus: "no_pago"})
	if err != nil || res.Total != 1 {
		t.Fatalf("filtro por estado de pago: %v %+v", err, res)
	}
	res, _ = f.s.ListOrders(OrderFilter{Q: "pérez"})
	if res.Total != 1 {
		t.Errorf("búsqueda por cliente: %+v", res)
	}
	res, _ = f.s.ListOrders(OrderFilter{Q: "zzz"})
	if res.Total != 0 {
		t.Errorf("búsqueda sin resultados: %+v", res)
	}
	rows, err := f.s.ListCustomers("1234", 10)
	if err != nil || len(rows) != 1 || rows[0].Orders != 1 {
		t.Fatalf("clientes: %v %+v", err, rows)
	}
}
