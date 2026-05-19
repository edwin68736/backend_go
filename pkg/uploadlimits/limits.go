package uploadlimits

// MaxFileBytes límite por archivo en handlers multipart (productos, contactos, comprobantes).
const MaxFileBytes = 10 << 20 // 10 MB

// RecommendedBodyLimitBytes límite Fiber (multipart overhead + campos del form).
const RecommendedBodyLimitBytes = 12 << 20 // 12 MB
