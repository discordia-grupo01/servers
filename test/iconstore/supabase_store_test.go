package iconstore_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/model"
)

func fakeSupabase(t *testing.T, wantKey string) *httptest.Server {
	t.Helper()
	objects := map[string][]byte{}
	contentTypes := map[string]string{}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantKey {
			t.Errorf("Authorization = %q, want Bearer %s", got, wantKey)
		}
		if got := r.Header.Get("apikey"); got != wantKey {
			t.Errorf("apikey = %q, want %s", got, wantKey)
		}

		id := r.URL.Path[len("/storage/v1/object/icons/"):]

		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			objects[id] = data
			contentTypes[id] = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, ok := objects[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", contentTypes[id])
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		case http.MethodDelete:
			delete(objects, id)
			delete(contentTypes, id)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func TestSupabaseStore_SaveThenGet_RoundTrips(t *testing.T) {
	srv := fakeSupabase(t, "test-service-key")
	defer srv.Close()

	st := iconstore.NewSupabaseStore(srv.URL, "icons", "test-service-key", max)

	url, err := st.Save("srv1", png(64))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := srv.URL + "/storage/v1/object/public/icons/srv1?v="
	if !strings.HasPrefix(url, want) {
		t.Errorf("url = %q, want prefix %q", url, want)
	}

	data, contentType, err := st.Get("srv1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", contentType)
	}
	if len(data) == 0 {
		t.Error("Get returned no data")
	}
}

func TestSupabaseStore_Save_ReturnsAFreshVersionWhenContentChanges(t *testing.T) {
	srv := fakeSupabase(t, "test-service-key")
	defer srv.Close()

	st := iconstore.NewSupabaseStore(srv.URL, "icons", "test-service-key", max)

	first, err := st.Save("srv1-banner", png(64))
	if err != nil {
		t.Fatalf("first Save: %v", err)
	}
	second, err := st.Save("srv1-banner", jpeg(64))
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}

	if first == second {
		t.Errorf("two saves returned the same URL (%q): a client or CDN caching by URL would never fetch the new content", first)
	}

	base := srv.URL + "/storage/v1/object/public/icons/srv1-banner?v="
	if !strings.HasPrefix(first, base) || !strings.HasPrefix(second, base) {
		t.Errorf("first=%q second=%q, want both to start with %q", first, second, base)
	}
	if _, _, err := st.Get("srv1-banner"); err != nil {
		t.Errorf("Get after two saves: %v", err)
	}
}

func TestSupabaseStore_Save_ReturnsTheSameVersionWhenContentIsUnchanged(t *testing.T) {
	srv := fakeSupabase(t, "test-service-key")
	defer srv.Close()

	st := iconstore.NewSupabaseStore(srv.URL, "icons", "test-service-key", max)
	data := png(64)

	first, err := st.Save("srv1-banner", data)
	if err != nil {
		t.Fatalf("first Save: %v", err)
	}
	second, err := st.Save("srv1-banner", data)
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}

	if first != second {
		t.Errorf("saving identical content twice produced different URLs: first=%q second=%q", first, second)
	}
}

func TestSupabaseStore_Get_UnknownID_ReturnsErrIconNotFound(t *testing.T) {
	srv := fakeSupabase(t, "test-service-key")
	defer srv.Close()

	st := iconstore.NewSupabaseStore(srv.URL, "icons", "test-service-key", max)

	if _, _, err := st.Get("nope"); !errors.Is(err, model.ErrIconNotFound) {
		t.Errorf("err = %v, want ErrIconNotFound", err)
	}
}

func TestSupabaseStore_Delete_ThenGet_ReturnsErrIconNotFound(t *testing.T) {
	srv := fakeSupabase(t, "test-service-key")
	defer srv.Close()

	st := iconstore.NewSupabaseStore(srv.URL, "icons", "test-service-key", max)

	if _, err := st.Save("srv1", png(64)); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("srv1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := st.Get("srv1"); !errors.Is(err, model.ErrIconNotFound) {
		t.Errorf("err = %v, want ErrIconNotFound", err)
	}
}

func TestSupabaseStore_Save_InvalidIcon_DoesNotUpload(t *testing.T) {
	srv := fakeSupabase(t, "test-service-key")
	defer srv.Close()

	st := iconstore.NewSupabaseStore(srv.URL, "icons", "test-service-key", max)

	if _, err := st.Save("srv2", gif(64)); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := st.Get("srv2"); !errors.Is(err, model.ErrIconNotFound) {
		t.Error("an invalid icon was uploaded anyway")
	}
}
