package facturador

import "bytes"

// FixNoteLanguageLocaleID reemplaza languageLocaleID por languageID en nodos cbc:Note.
// Útil si se dispone del XML antes del envío (p. ej. post-proceso en servidor Lycet).
// El backend Tukifac evita el problema enviando la leyenda vía observacion en el JSON a Lycet.
func FixNoteLanguageLocaleID(xml []byte) []byte {
	return bytes.ReplaceAll(xml, []byte(`languageLocaleID="`), []byte(`languageID="`))
}
