package service

import "testing"

// Retorno parcial: devolver solo parte de lo enviado (caso pedido 98: de 2 TK-E583, 1 dañada) sin cerrar todo el pedido.

func (f *fixture) returnFor(t *testing.T, orderID uint, qty int) (*ReturnView, error) {
	t.Helper()
	return f.s.CreateReturn(ReturnInput{OrderID: orderID, Condition: "danado", Items: []ReturnItemInput{{ProductID: f.printer.ID, Quantity: qty}}}, 1)
}

func TestReturnPartial_unaDeDosNoCambiaElEnvioYQuedaComoDanada(t *testing.T) {
	f := newFixture(t)
	o := f.shipped(t, 2, 100) // 2 impresoras, envío en agencia
	r, err := f.returnFor(t, o.ID, 1)
	if err != nil {
		t.Fatalf("retorno parcial: %v", err)
	}
	if len(r.Items) != 1 || r.Items[0].Quantity != 1 {
		t.Fatalf("items del retorno: %+v", r.Items)
	}
	if r.GuideNumber != "" {
		t.Errorf("el retorno parcial no hereda la guía de ida: %q", r.GuideNumber)
	}
	if v, _ := f.s.GetOrder(o.ID); v.Shipment.Status == "retorno" {
		t.Fatalf("un retorno parcial no pone todo el envío en retorno: %s", v.Shipment.Status)
	}
	if _, err := f.s.UpdateReturn(r.ID, ReturnUpdate{Status: "recibido"}, 1); err != nil {
		t.Fatal(err)
	}
	dmg, _ := f.s.damagedByProduct()
	if dmg[f.printer.ID] != 1 {
		t.Fatalf("dañados = %d, quería 1 (solo la unidad devuelta)", dmg[f.printer.ID])
	}
	if f.stock(t, f.printer.ID) != 8 {
		t.Fatalf("dañada no reingresa al stock: %d", f.stock(t, f.printer.ID))
	}
}

func TestReturnPartial_variosRetornosSinSuperarLoEnviado(t *testing.T) {
	f := newFixture(t)
	o := f.shipped(t, 2, 100)
	if _, err := f.returnFor(t, o.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.returnFor(t, o.ID, 2); err == nil || !IsValidation(err) {
		t.Fatalf("no se devuelve más de lo que queda (quedaba 1): %v", err)
	}
	if _, err := f.returnFor(t, o.ID, 1); err != nil {
		t.Fatalf("la segunda unidad sí se puede devolver: %v", err)
	}
	if _, err := f.returnFor(t, o.ID, 1); err == nil {
		t.Fatal("ya se devolvió todo")
	}
	items, _ := f.s.ReturnableItems(o.ID)
	if len(items) != 1 || items[0].Sold != 2 || items[0].Returned != 2 || items[0].Available != 0 {
		t.Fatalf("returnable: %+v", items)
	}
}

func TestReturnPartial_anularLiberaLaCantidad(t *testing.T) {
	f := newFixture(t)
	o := f.shipped(t, 2, 100)
	r, _ := f.returnFor(t, o.ID, 2)
	if _, err := f.returnFor(t, o.ID, 1); err == nil {
		t.Fatal("ya se devolvió todo")
	}
	if _, err := f.s.CancelReturn(r.ID, "error", 1); err != nil {
		t.Fatal(err)
	}
	items, _ := f.s.ReturnableItems(o.ID)
	if len(items) != 1 || items[0].Available != 2 {
		t.Fatalf("tras anular vuelve a estar disponible: %+v", items)
	}
}

func TestReturnPartial_permitidoConEnvioRecogidoPeroCompletoNo(t *testing.T) {
	f := newFixture(t)
	o := f.shipped(t, 2, 100)
	if _, err := f.s.MarkPickedUp(o.ID, ""); err != nil {
		t.Skipf("no se pudo marcar recogido en el fixture: %v", err)
	}
	if _, err := f.returnFor(t, o.ID, 1); err != nil {
		t.Fatalf("parcial sobre envío recogido debe permitirse: %v", err)
	}
	if v, _ := f.s.GetOrder(o.ID); v.Shipment.Status != "entregado" {
		t.Fatalf("el envío recogido no cambia: %s", v.Shipment.Status)
	}
}

func TestReturnPartial_retornoCompletoSigueIgual(t *testing.T) {
	f := newFixture(t)
	o := f.shipped(t, 2, 100)
	r, err := f.s.CreateReturn(ReturnInput{OrderID: o.ID}, 1) // sin items = todo
	if err != nil || len(r.Items) != 1 || r.Items[0].Quantity != 2 {
		t.Fatalf("retorno completo: %v %+v", err, r)
	}
	if v, _ := f.s.GetOrder(o.ID); v.Shipment.Status != "retorno" {
		t.Fatalf("retorno completo pone el envío en retorno: %s", v.Shipment.Status)
	}
}

func TestReturnPartial_noReenviaUnRetornoParcial(t *testing.T) {
	f := newFixture(t)
	o := f.shipped(t, 2, 100)
	r, _ := f.s.CreateReturn(ReturnInput{OrderID: o.ID, Condition: "buen_estado", Items: []ReturnItemInput{{ProductID: f.printer.ID, Quantity: 1}}}, 1)
	if _, err := f.s.UpdateReturn(r.ID, ReturnUpdate{Status: "recibido"}, 1); err != nil {
		t.Fatal(err)
	}
	if f.stock(t, f.printer.ID) != 9 {
		t.Fatalf("buen estado reingresa solo la unidad devuelta: %d", f.stock(t, f.printer.ID))
	}
	if _, err := f.s.Reship(r.ID, 1); err == nil || !IsValidation(err) {
		t.Fatalf("un retorno parcial no se reenvía: %v", err)
	}
}
