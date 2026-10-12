package role_handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func roleMembersPath(roleID string, query string) string {
	p := "/v1/roles/" + roleID + "/members"
	if query != "" {
		p += "?" + query
	}
	return p
}

func TestListRoleMembers_ReturnsThePage(t *testing.T) {
	roleID := uuid.New()
	fake := &fakeRoleUseCases{roleMembers: []model.Member{{UserID: "u1"}}, roleMembersTotal: 7}

	w := doJSON(t, newRouter(t, fake), http.MethodGet, roleMembersPath(roleID.String(), "limit=5&offset=5"), "mod-1", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body model.MemberListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 7 || body.Limit != 5 || body.Offset != 5 || len(body.Members) != 1 || body.Members[0].UserID != "u1" {
		t.Errorf("body = %+v", body)
	}
	if fake.gotRoleMembers.RoleID != roleID || fake.gotRoleMembers.UserID != "mod-1" || fake.gotRoleMembers.Limit != 5 {
		t.Errorf("service got %+v", fake.gotRoleMembers)
	}
}

func TestListRoleMembers_Errors(t *testing.T) {
	cases := map[string]struct {
		path   string
		user   string
		err    error
		status int
	}{
		"no user":         {roleMembersPath(uuid.NewString(), ""), "", nil, http.StatusUnauthorized},
		"bad role id":     {roleMembersPath("nope", ""), "mod-1", nil, http.StatusBadRequest},
		"bad limit":       {roleMembersPath(uuid.NewString(), "limit=-1"), "mod-1", nil, http.StatusBadRequest},
		"no MANAGE_ROLES": {roleMembersPath(uuid.NewString(), ""), "mod-1", model.ErrForbiddenNotAdmin, http.StatusForbidden},
		"role not found":  {roleMembersPath(uuid.NewString(), ""), "mod-1", model.ErrRoleNotFound, http.StatusNotFound},
	}
	for name, tc := range cases {
		w := doJSON(t, newRouter(t, &fakeRoleUseCases{roleMembersErr: tc.err}), http.MethodGet, tc.path, tc.user, nil)
		if w.Code != tc.status {
			t.Errorf("%s: status = %d, want %d", name, w.Code, tc.status)
		}
	}
}
