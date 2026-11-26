package service_test

import "testing"

func keyOf(t *testing.T, name string) string {
	t.Helper()
	srv, err, _, _ := create(t, validParams(name))
	if err != nil {
		t.Fatalf("name %q rejected: %v", name, err)
	}
	return srv.NameKey
}

func TestNameKey_LowercasesTheName(t *testing.T) {
	if got := keyOf(t, "MI COMUNIDAD"); got != "mi comunidad" {
		t.Errorf("name_key = %q, want %q", got, "mi comunidad")
	}
}

func TestNameKey_StripsAccents(t *testing.T) {
	if got := keyOf(t, "Ñandú Café"); got != "nandu cafe" {
		t.Errorf("name_key = %q, want %q", got, "nandu cafe")
	}
}

func TestNameKey_CollapsesInternalWhitespace(t *testing.T) {
	if got := keyOf(t, "mi   comunidad"); got != "mi comunidad" {
		t.Errorf("name_key = %q, want %q", got, "mi comunidad")
	}
}

func TestNameKey_IgnoresSurroundingWhitespace(t *testing.T) {
	if got := keyOf(t, "  Hola Mundo  "); got != "hola mundo" {
		t.Errorf("name_key = %q, want %q", got, "hola mundo")
	}
}

func TestNameKey_IsEqualForCaseAccentAndSpacingVariants(t *testing.T) {
	base := keyOf(t, "Café Central")
	for _, variant := range []string{"CAFÉ CENTRAL", "cafe central", "  Café   Central  ", "café  central"} {
		if got := keyOf(t, variant); got != base {
			t.Errorf("variant %q => key %q, want %q", variant, got, base)
		}
	}
}
