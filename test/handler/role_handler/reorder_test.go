package role_handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

func reorderPath(serverID uuid.UUID) string {
	return "/v1/servers/" + serverID.String() + "/roles/reorder"
}

func TestReorderRoles_Returns200WithTheNewHierarchy(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	fake := &fakeRoleUseCases{reorderResult: []role.Role{
		{ID: b, Name: "B", Color: "#000000", Position: 1},
		{ID: a, Name: "A", Color: "#000000", Position: 2},
	}}
	r := newRouter(t, fake)
	serverID := uuid.New()

	w := doJSON(t, r, http.MethodPatch, reorderPath(serverID), testUser,
		map[string]any{"role_ids": []string{b.String(), a.String()}})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotReorder.ServerID != serverID || fake.gotReorder.UserID != testUser ||
		len(fake.gotReorder.RoleIDs) != 2 || fake.gotReorder.RoleIDs[0] != b {
		t.Errorf("service got %+v", fake.gotReorder)
	}
	var got []roleJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	if len(got) != 2 || got[0].Name != "B" || got[0].Position != 1 || got[1].Position != 2 {
		t.Errorf("roles = %+v", got)
	}
}

func TestReorderRoles_EmptyListIsAccepted(t *testing.T) {
	fake := &fakeRoleUseCases{}
	r := newRouter(t, fake)
	w := doJSON(t, r, http.MethodPatch, reorderPath(uuid.New()), testUser, map[string]any{"role_ids": []string{}})
	if w.Code != http.StatusOK || w.Body.String() != "[]" {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReorderRoles_BadRequests(t *testing.T) {
	cases := map[string]struct {
		body   any
		field  string
		reason string
	}{
		"body is not an object": {`{"role_ids": [`, "", "invalid_json"},
		"missing role_ids":      {map[string]any{}, "role_ids", "required"},
		"non-uuid role id":      {map[string]any{"role_ids": []string{"nope"}}, "role_ids", "invalid_uuid"},
	}
	for name, tc := range cases {
		fake := &fakeRoleUseCases{}
		r := newRouter(t, fake)
		w := doJSON(t, r, http.MethodPatch, reorderPath(uuid.New()), testUser, tc.body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: code=%d body=%s", name, w.Code, w.Body.String())
			continue
		}
		e := decodeError(t, w)
		if e.Error.Details["reason"] != tc.reason || (tc.field != "" && e.Error.Details["field"] != tc.field) {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
		if fake.reorderCalled {
			t.Errorf("%s: service called on a bad request", name)
		}
	}
}

func TestReorderRoles_InvalidServerIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPatch, "/v1/servers/nope/roles/reorder", testUser, map[string]any{"role_ids": []string{}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestReorderRoles_DomainErrorsMapToHTTP(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		reason string
	}{
		"list does not match":  {model.ErrRoleReorderInvalid, http.StatusBadRequest, "reorder_invalid"},
		"role above own (CA2)": {model.ErrRoleReorderAboveOwn, http.StatusForbidden, "reorder_above_own"},
		"not an administrator": {model.ErrForbiddenNotAdmin, http.StatusForbidden, ""},
		"server not found":     {model.ErrServerNotFound, http.StatusNotFound, ""},
	}
	for name, tc := range cases {
		r := newRouter(t, &fakeRoleUseCases{reorderErr: tc.err})
		w := doJSON(t, r, http.MethodPatch, reorderPath(uuid.New()), testUser, map[string]any{"role_ids": []string{}})
		if w.Code != tc.status {
			t.Errorf("%s: code=%d, want %d", name, w.Code, tc.status)
			continue
		}
		if tc.reason != "" {
			if e := decodeError(t, w); e.Error.Details["reason"] != tc.reason {
				t.Errorf("%s: details = %v", name, e.Error.Details)
			}
		}
	}
}

func TestReorderRoles_MissingIdentity_Returns401(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPatch, reorderPath(uuid.New()), "", map[string]any{"role_ids": []string{}})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}
