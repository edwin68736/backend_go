package service

import "encoding/json"

// enrichFiscalPayloadJSON añade tipoDoc y _meta para el deserializador Greenter en facturador SSOT.
func enrichFiscalPayloadJSON(payloadJSON, tipoDoc, documentKind string) string {
	if payloadJSON == "" {
		return payloadJSON
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(payloadJSON), &m) != nil {
		return payloadJSON
	}
	if tipoDoc != "" {
		m["tipoDoc"] = tipoDoc
	}
	if documentKind != "" {
		m["_meta"] = map[string]string{"document_kind": documentKind}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return payloadJSON
	}
	return string(b)
}
