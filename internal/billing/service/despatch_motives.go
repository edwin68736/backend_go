package service

import (
	"fmt"
	"strings"

	"tukifac/pkg/database"
	"tukifac/pkg/facturador"
)

// Motivos de traslado (catálogo 20) que el formulario de guías soporta hoy. El resto
// (importación, exportación, consignación, venta con entrega a terceros…) requiere datos
// adicionales que todavía no se capturan; se rechazan con un mensaje claro en vez de generar
// un XML que SUNAT rechazaría.
const (
	greMotivoVenta         = "01"
	greMotivoCompra        = "02"
	greMotivoTrasladoLocal = "04"
	greMotivoDevolucion    = "06"
	greMotivoOtros         = "13"

	// greCodLocalPrincipal establecimiento anexo "0000" (domicilio fiscal) del catálogo SUNAT.
	greCodLocalPrincipal = "0000"
	greObservacionMax    = 250
	greDesTrasladoMax    = 100

	greIndM1L                = "SUNAT_Envio_IndicadorTrasladoVehiculoM1L"
	greIndRetornoEnvases     = "SUNAT_Envio_IndicadorRetornoVehiculoEnvaseVacio"
	greIndRetornoVacio       = "SUNAT_Envio_IndicadorRetornoVehiculoVacio"
	greIndTransbordo         = "SUNAT_Envio_IndicadorTransbordoProgramado"
	greIndPagaFleteRemitente = "SUNAT_Envio_IndicadorPagadorFlete_Remitente"
)

var greMotivosSoportados = map[string]string{
	greMotivoVenta:         "Venta",
	greMotivoCompra:        "Compra",
	greMotivoTrasladoLocal: "Traslado entre establecimientos de la misma empresa",
	greMotivoDevolucion:    "Devolución",
	greMotivoOtros:         "Otros",
}

// despatchMotivo devuelve el código de motivo efectivo (vacío = venta).
func despatchMotivo(env DespatchEnvioInput) string {
	m := strings.TrimSpace(env.CodTraslado)
	if m == "" {
		return greMotivoVenta
	}
	return m
}

// despatchDestinatarioEsEmpresa indica los motivos en que el destinatario ES la empresa emisora:
// en el traslado entre establecimientos (04) la mercadería va a otro local propio, y en la compra
// (02) la empresa recoge lo que le vende el proveedor (que figura como tercero).
func despatchDestinatarioEsEmpresa(sunatCode, motivo string) bool {
	return sunatCode == "09" && (motivo == greMotivoTrasladoLocal || motivo == greMotivoCompra)
}

// normalizeDespatchMotivo valida el motivo y completa la descripción por defecto. Solo aplica a la
// guía remitente: la transportista no tiene motivo propio y conserva "01 Venta" como hasta ahora.
func normalizeDespatchMotivo(input *CreateDespatchInput, sunatCode string) error {
	if sunatCode != "09" {
		if strings.TrimSpace(input.Envio.CodTraslado) == "" {
			input.Envio.CodTraslado = greMotivoVenta
		}
		if strings.TrimSpace(input.Envio.DesTraslado) == "" {
			input.Envio.DesTraslado = greMotivosSoportados[greMotivoVenta]
		}
		return nil
	}
	motivo := despatchMotivo(input.Envio)
	nombre, ok := greMotivosSoportados[motivo]
	if !ok {
		return fmt.Errorf("el motivo de traslado %s todavía no está disponible; use venta, traslado entre locales, compra, devolución u otros", motivo)
	}
	input.Envio.CodTraslado = motivo
	des := strings.TrimSpace(input.Envio.DesTraslado)
	if motivo == greMotivoOtros {
		if des == "" || strings.EqualFold(des, nombre) {
			return fmt.Errorf("describa el motivo del traslado (obligatorio cuando el motivo es «Otros»)")
		}
	} else if des == "" {
		des = nombre
	}
	if len([]rune(des)) > greDesTrasladoMax {
		des = string([]rune(des)[:greDesTrasladoMax])
	}
	input.Envio.DesTraslado = des
	return nil
}

// applyDespatchMotivoParties fija las partes que el motivo determina: destinatario = empresa en
// traslado entre locales y compra. Se hace en el servidor para que ningún cliente pueda emitir
// una guía 04 o 02 con un destinatario ajeno.
func applyDespatchMotivoParties(input *CreateDespatchInput, sunatCode, ruc, razon, address, ubigeo string) {
	if !despatchDestinatarioEsEmpresa(sunatCode, despatchMotivo(input.Envio)) {
		return
	}
	input.Destinatario = DespatchDestinatarioInput{
		TipoDoc:   "6",
		NumDoc:    strings.TrimSpace(ruc),
		RznSocial: strings.TrimSpace(razon),
		Address:   strings.TrimSpace(address),
		Ubigeo:    strings.TrimSpace(ubigeo),
	}
	if despatchMotivo(input.Envio) == greMotivoTrasladoLocal {
		input.Tercero = nil
	}
}

func normalizeGreAddress(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// validateDespatchMotivoRules reglas de negocio por motivo (solo guía remitente).
func validateDespatchMotivoRules(input CreateDespatchInput, sunatCode string) error {
	if sunatCode != "09" {
		return nil
	}
	env := input.Envio
	switch despatchMotivo(env) {
	case greMotivoTrasladoLocal:
		sameUbigeo := strings.TrimSpace(env.PartidaUbigueo) == strings.TrimSpace(env.LlegadaUbigueo)
		sameDir := normalizeGreAddress(env.PartidaDireccion) == normalizeGreAddress(env.LlegadaDireccion)
		if sameUbigeo && sameDir {
			return fmt.Errorf("en un traslado entre locales el destino debe ser distinto del origen: elija otra sucursal de llegada")
		}
	case greMotivoCompra:
		t := input.Tercero
		if t == nil || strings.TrimSpace(t.NumDoc) == "" || strings.TrimSpace(t.RznSocial) == "" {
			return fmt.Errorf("en una compra indique el proveedor (documento y razón social)")
		}
		if strings.TrimSpace(t.NumDoc) == strings.TrimSpace(input.Destinatario.NumDoc) {
			return fmt.Errorf("el proveedor no puede ser la misma empresa emisora")
		}
	}
	return nil
}

func appendUniqueString(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// despatchExtraIndicators indicadores opcionales (M1/L, retornos, transbordo, flete).
func despatchExtraIndicators(env DespatchEnvioInput, sunatCode string) []string {
	var out []string
	if sunatCode == "09" && env.IndM1L {
		out = append(out, greIndM1L)
	}
	if env.IndRetornoEnvases {
		out = append(out, greIndRetornoEnvases)
	}
	if env.IndRetornoVehiculoVacio {
		out = append(out, greIndRetornoVacio)
	}
	if env.IndTransbordo {
		out = append(out, greIndTransbordo)
	}
	if sunatCode == "31" && env.PagaFleteRemitente {
		out = append(out, greIndPagaFleteRemitente)
	}
	return out
}

// buildDespatchSecondaryDriver conductor secundario (opcional): todo o nada.
func buildDespatchSecondaryDriver(c *DespatchChoferInput) (*facturador.DespatchDriver, error) {
	if c == nil {
		return nil, nil
	}
	doc := strings.TrimSpace(c.Doc)
	lic := normalizeGreLicencia(c.Licencia)
	if doc == "" && lic == "" {
		return nil, nil
	}
	if err := validateDespatchDriverFields(DespatchEnvioInput{ChoferDoc: doc, ChoferLicencia: lic}, true); err != nil {
		return nil, fmt.Errorf("segundo conductor: %w", err)
	}
	tipo := strings.TrimSpace(c.TipoDoc)
	if tipo == "" {
		tipo = "1"
	}
	nombres := strings.TrimSpace(c.Nombres)
	apellidos := strings.TrimSpace(c.Apellidos)
	if nombres == "" && apellidos == "" {
		nombres = "CONDUCTOR"
	}
	return &facturador.DespatchDriver{Tipo: "Secundario", TipoDoc: tipo, NroDoc: doc, Nombres: nombres, Apellidos: apellidos, Licencia: lic}, nil
}

// buildDespatchRelatedDocs documentos relacionados manuales (catálogo 61).
func buildDespatchRelatedDocs(in []DespatchRelatedDocInput) ([]facturador.DespatchAdditionalDoc, error) {
	var out []facturador.DespatchAdditionalDoc
	for _, d := range in {
		nro := strings.ToUpper(strings.TrimSpace(d.Nro))
		if nro == "" {
			continue
		}
		tipo := strings.TrimSpace(d.Tipo)
		if tipo == "" {
			tipo = "09"
		}
		desc := "Documento relacionado"
		switch tipo {
		case "01":
			desc = "Factura"
		case "03":
			desc = "Boleta de Venta"
		case "09":
			desc = "Guía de Remisión Remitente"
		}
		out = append(out, facturador.DespatchAdditionalDoc{Tipo: tipo, Nro: nro, TipoDesc: desc, Emisor: strings.TrimSpace(d.Emisor)})
	}
	return out, nil
}

// applyDespatchBranchDefaults usa el punto de partida de la sucursal de la guía (dirección y
// domicilio fiscal de la sucursal, con la empresa como último recurso) cuando el formulario no
// lo envió. En la compra el punto de partida es el del proveedor, por eso no se completa.
func (s *BillingService) applyDespatchBranchDefaults(input *CreateDespatchInput, sunatCode string, company *database.TenantCompanyConfig) {
	if sunatCode != "09" || despatchMotivo(input.Envio) == greMotivoCompra {
		return
	}
	env := &input.Envio
	if strings.TrimSpace(env.PartidaDireccion) != "" && strings.TrimSpace(env.PartidaUbigueo) != "" {
		return
	}
	var branch database.TenantBranch
	if input.BranchID == 0 || s.db.Select("id", "address", "fiscal_domicile_code").First(&branch, input.BranchID).Error != nil {
		branch = database.TenantBranch{}
	}
	if strings.TrimSpace(env.PartidaDireccion) == "" {
		env.PartidaDireccion = firstNonEmptyTrim(branch.Address, company.Address)
	}
	if strings.TrimSpace(env.PartidaUbigueo) == "" {
		code := strings.TrimSpace(branch.FiscalDomicileCode)
		if len(code) != 6 {
			code = strings.TrimSpace(company.Ubigeo)
		}
		env.PartidaUbigueo = code
	}
}

func firstNonEmptyTrim(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// fillRelatedGreIssuer en la guía transportista, la GRE remitente relacionada la emitió el
// remitente: SUNAT (3380) exige su RUC como emisor del documento relacionado.
func fillRelatedGreIssuer(docs []facturador.DespatchAdditionalDoc, sunatCode string, remitente DespatchDestinatarioInput) {
	if sunatCode != "31" {
		return
	}
	_, num := normalizeGrePartyDoc(remitente.TipoDoc, remitente.NumDoc)
	if len(num) != 11 {
		return
	}
	for i := range docs {
		if docs[i].Tipo == "09" && docs[i].Emisor == "" {
			docs[i].Emisor = num
		}
	}
}
