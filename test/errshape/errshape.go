// Package errshape valida la forma del cuerpo de error unificado
// ({"error": {"code", "message", "details"}}) en los tests de cada paquete.
package errshape

import (
	"encoding/json"
	"regexp"
	"testing"
)

var codeRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ValidCode indica si code cumple ^[A-Z][A-Z0-9_]*$.
func ValidCode(code string) bool { return codeRE.MatchString(code) }

// CheckCode falla el test si code no es SCREAMING_SNAKE_CASE.
func CheckCode(t testing.TB, code string, status int) {
	t.Helper()
	if !ValidCode(code) {
		t.Errorf("error.code=%q (HTTP %d) no cumple ^[A-Z][A-Z0-9_]*$", code, status)
	}
}

// Envelope valida la forma completa del cuerpo: única clave "error" de primer
// nivel; code string válido; message string no vacío; details objeto (nunca
// null). Devuelve el code.
func Envelope(t testing.TB, body []byte) string {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("el cuerpo de error no es un objeto JSON: %q: %v", body, err)
	}
	if len(top) != 1 {
		t.Errorf("el cuerpo debe tener solo la clave error, tiene %d claves: %s", len(top), body)
	}
	raw, ok := top["error"]
	if !ok {
		t.Fatalf("falta la clave error: %s", body)
	}
	var e struct {
		Code    any             `json:"code"`
		Message any             `json:"message"`
		Details json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("error mal formado: %s: %v", raw, err)
	}
	code, ok := e.Code.(string)
	if !ok {
		t.Fatalf("error.code debe ser string, es %T (%v)", e.Code, e.Code)
	}
	CheckCode(t, code, 0)
	if msg, ok := e.Message.(string); !ok || msg == "" {
		t.Errorf("error.message debe ser un string no vacío, es %v", e.Message)
	}
	var details map[string]any
	if len(e.Details) == 0 || e.Details[0] != '{' || json.Unmarshal(e.Details, &details) != nil {
		t.Errorf("error.details debe ser un objeto, es %s", e.Details)
	}
	return code
}
