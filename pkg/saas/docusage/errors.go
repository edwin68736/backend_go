package docusage

import "errors"

var (
	ErrQuotaExceeded          = errors.New("Has alcanzado el límite de documentos electrónicos de tu plan. Compra un paquete o mejora tu plan.")
	ErrUnlimitedCannotBuyPkg  = errors.New("su plan incluye documentos ilimitados; no puede comprar paquetes adicionales")
	ErrPackageNotFound        = errors.New("paquete no encontrado o inactivo")
	ErrNoActiveCycle          = errors.New("no hay ciclo de facturación vigente para documentos")
	ErrPackageAlreadyReviewed = errors.New("la solicitud de paquete ya fue procesada")
	ErrTenantRequired         = errors.New("tenant SaaS no identificado: no se puede validar cupo de documentos")
)
