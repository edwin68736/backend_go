package service

import "testing"

func TestOrder_agencyRequiredForAgencyDelivery(t *testing.T) {
	f := newFixture(t)

	noAgency := f.input(1, 100)
	noAgency.Shipment.DestinationAgency = ""
	if _, err := f.s.CreateOrder(noAgency, 1); err == nil || !IsValidation(err) {
		t.Fatalf("un envío por agencia sin agencia de destino no se guarda: %v", err)
	}

	// Escrita a mano también vale (la agencia puede no estar en la lista cargada).
	typed := f.input(1, 100)
	typed.Shipment.DestinationAgency = "Shalom Av. Lima 123"
	if _, err := f.s.CreateOrder(typed, 1); err != nil {
		t.Fatalf("agencia escrita a mano: %v", err)
	}

	// Recojo en oficina no necesita agencia.
	office := f.input(1, 100)
	office.Shipment = &ShipmentInput{DeliveryMode: "oficina"}
	if _, err := f.s.CreateOrder(office, 1); err != nil {
		t.Fatalf("recojo en oficina sin agencia: %v", err)
	}
}

func TestOrder_updateKeepsAgencyRule(t *testing.T) {
	f := newFixture(t)
	r, err := f.s.CreateOrder(f.input(1, 100), 1)
	if err != nil {
		t.Fatal(err)
	}
	in := f.input(1, 100)
	in.Shipment.DestinationAgency = ""
	if _, err := f.s.UpdateOrder(r.Order.ID, in, ConfirmInput{}, 1); err == nil || !IsValidation(err) {
		t.Fatalf("editar quitando la agencia debe fallar: %v", err)
	}
}

func TestOrder_pickupContactDocTypes(t *testing.T) {
	f := newFixture(t)

	ce := f.input(1, 100)
	ce.ContactDocType, ce.ContactDNI, ce.ContactName = "CE", "001234567", "Carlos Extranjero"
	r, err := f.s.CreateOrder(ce, 1)
	if err != nil {
		t.Fatalf("carné de extranjería de quien recoge: %v", err)
	}
	if r.Order.ContactDocType != "CE" || r.Order.ContactDNI != "001234567" {
		t.Fatalf("no se guardó el CE: %q %q", r.Order.ContactDocType, r.Order.ContactDNI)
	}

	bad := f.input(1, 100)
	bad.ContactDocType, bad.ContactDNI, bad.ContactName = "CE", "123", "Corto"
	if _, err := f.s.CreateOrder(bad, 1); err == nil || !IsValidation(err) {
		t.Fatalf("un CE de 3 caracteres es inválido: %v", err)
	}

	dni := f.input(1, 100)
	dni.ContactDNI, dni.ContactName = "1234567", "Falta un dígito"
	if _, err := f.s.CreateOrder(dni, 1); err == nil || !IsValidation(err) {
		t.Fatalf("un DNI de 7 dígitos es inválido: %v", err)
	}

	unknown := f.input(1, 100)
	unknown.ContactDocType, unknown.ContactDNI, unknown.ContactName = "PAS", "AB1234567", "Otro"
	if _, err := f.s.CreateOrder(unknown, 1); err == nil || !IsValidation(err) {
		t.Fatalf("tipo de documento desconocido: %v", err)
	}
}

func TestOrder_searchFindsPickupPerson(t *testing.T) {
	f := newFixture(t)
	in := f.input(1, 100)
	in.ContactDNI, in.ContactName = "87654321", "Rosa Quispe Mamani"
	if _, err := f.s.CreateOrder(in, 1); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"quispe", "87654321"} {
		res, err := f.s.ListOrders(OrderFilter{Q: q})
		if err != nil || res.Total != 1 {
			t.Fatalf("buscar %q por quien recoge: total=%v err=%v", q, res, err)
		}
	}
	if res, _ := f.s.ListOrders(OrderFilter{Q: "Juan"}); res == nil || res.Total != 1 {
		t.Fatal("la búsqueda por nombre del cliente sigue funcionando")
	}
}
