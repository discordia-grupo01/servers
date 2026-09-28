package role_handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

func TestCreateRole_ResponseIncludesPosition(t *testing.T) {
	fake := &fakeRoleUseCases{created: role.Role{ID: uuid.New(), Name: "Moderator", Color: "#FF00AA", Position: 3}}
	r := newRouter(t, fake)
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/roles", testUser,
		map[string]string{"name": "Moderator", "color": "#FF00AA"})
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeRole(t, w); got.Position != 3 {
		t.Errorf("position = %d, want 3", got.Position)
	}
}

func TestListRoles_KeepsTheServiceOrderAndExposesPositions(t *testing.T) {
	fake := &fakeRoleUseCases{listResult: []role.Role{
		{ID: uuid.New(), Name: "Admin", Color: "#000000", Position: 1},
		{ID: uuid.New(), Name: "Member", Color: "#FFFFFF", Position: 2},
	}}
	r := newRouter(t, fake)
	w := doJSON(t, r, http.MethodGet, "/v1/servers/"+uuid.New().String()+"/roles", testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var got []roleJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	if len(got) != 2 || got[0].Name != "Admin" || got[0].Position != 1 || got[1].Name != "Member" || got[1].Position != 2 {
		t.Errorf("roles = %+v", got)
	}
}
