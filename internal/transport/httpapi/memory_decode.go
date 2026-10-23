package httpapi

import (
	"bytes"
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/domain"
	"io"
	"net/http"
)

// Stored-format losslessness does not relax strict public request schemas.
func decodeMemory(w http.ResponseWriter, r *http.Request, value any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		writeErr(w, domain.TooLarge("memory request exceeds 1 MiB"))
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		writeErr(w, domain.Invalid("memory request must be a JSON object"))
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		writeErr(w, domain.Invalid("invalid memory request: unknown field or malformed JSON"))
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeErr(w, domain.Invalid("memory request must contain one JSON object"))
		return false
	}
	return true
}
