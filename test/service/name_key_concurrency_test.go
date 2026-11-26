package service_test

import (
	"sync"
	"testing"

	"github.com/discordia-grupo01/servers/internal/service"
)

func TestNameKey_IsSafeForConcurrentUse(t *testing.T) {
	inputs := map[string]string{
		"Café Ñandú":      "cafe nandu",
		"  Hello   World": "hello world",
		"tC":              "tc",
		"Ünïcödé Séríes":  "unicode series",
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				for in, want := range inputs {
					if got := service.NameKey(in); got != want {
						t.Errorf("NameKey(%q) = %q, want %q", in, got, want)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}
