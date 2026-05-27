package modifierkind

import "strings"

const (
	Presentation = "presentation"
	Extra        = "extra"
)

// NormalizeKind devuelve presentation o extra. Si kind está vacío, infiere por flags legacy.
func NormalizeKind(kind string, required, multiSelect bool) string {
	k := strings.TrimSpace(strings.ToLower(kind))
	if k == Presentation || k == Extra {
		return k
	}
	if required && !multiSelect {
		return Presentation
	}
	return Extra
}

func IsPresentation(kind string, required, multiSelect bool) bool {
	return NormalizeKind(kind, required, multiSelect) == Presentation
}

func IsExtra(kind string, required, multiSelect bool) bool {
	return NormalizeKind(kind, required, multiSelect) == Extra
}

// EntryTypeJSON es el valor en modifiers_json: variant = presentación, modifier = extra.
func EntryTypeJSON(kind string, required, multiSelect bool) string {
	if IsPresentation(kind, required, multiSelect) {
		return "variant"
	}
	return "modifier"
}
