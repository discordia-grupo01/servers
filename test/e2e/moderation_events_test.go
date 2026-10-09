package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/membership"
	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
)

func doJSONAsUser(t *testing.T, r http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// nextDeliveries collects the next n messages, in whatever order the outbox
// poller published them.
func nextDeliveries(t *testing.T, deliveries <-chan amqp.Delivery, n int) map[string]amqp.Delivery {
	t.Helper()
	got := make(map[string]amqp.Delivery, n)
	for len(got) < n {
		select {
		case msg := <-deliveries:
			got[msg.RoutingKey] = msg
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out: got %d of %d events", len(got), n)
		}
	}
	return got
}

// #25 and #26 CA1, end to end over HTTP: the owner bans a member, both
// servers.member_left and servers.member_banned reach the discordia.events
// exchange, and the banned user can't rejoin with any invitation until the
// ban is revoked.
func TestModerationEvents_BanAndUnbanEndToEnd(t *testing.T) {
	router, deliveries := setupE2E(t)

	owner := "owner-" + uuid.NewString()
	target := "target-" + uuid.NewString()

	w := postCreateServer(t, router, owner, "E2E Ban Server")
	if w.Code != http.StatusCreated {
		t.Fatalf("create server: code=%d body=%s", w.Code, w.Body.String())
	}
	var srv struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &srv)
	waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined) // owner

	w = doAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/invites", owner)
	var inv struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &inv)
	if w = doAsUser(t, router, http.MethodPost, "/v1/invites/"+inv.Code+"/join", target); w.Code != http.StatusCreated {
		t.Fatalf("join: code=%d body=%s", w.Code, w.Body.String())
	}
	waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined) // target

	w = doJSONAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/bans", owner,
		`{"user_id":"`+target+`","reason":"spam"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("ban: code=%d body=%s", w.Code, w.Body.String())
	}

	got := nextDeliveries(t, deliveries, 2)
	left, ok := got[membership.RoutingKeyMemberLeft]
	if !ok {
		t.Fatalf("no member_left event, got %v", keys(got))
	}
	var leftEvt membership.MemberLeft
	_ = json.Unmarshal(left.Body, &leftEvt)
	if leftEvt.ServerID != srv.ID || leftEvt.UserID != target {
		t.Errorf("member_left = %+v", leftEvt)
	}
	banned, ok := got[moderationevents.RoutingKeyMemberBanned]
	if !ok {
		t.Fatalf("no member_banned event, got %v", keys(got))
	}
	var bannedEvt moderationevents.MemberBanned
	_ = json.Unmarshal(banned.Body, &bannedEvt)
	if bannedEvt.ServerID != srv.ID || bannedEvt.UserID != target || bannedEvt.BannedBy != owner || bannedEvt.EventID == "" {
		t.Errorf("member_banned = %+v", bannedEvt)
	}
	if bytes.Contains(banned.Body, []byte("spam")) {
		t.Error("the reason leaked into the member_banned event")
	}

	// Not a member anymore, and no invitation lets them back in (not even a
	// fresh one).
	w = doAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/invites", owner)
	var fresh struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &fresh)
	for _, code := range []string{inv.Code, fresh.Code} {
		if w = doAsUser(t, router, http.MethodPost, "/v1/invites/"+code+"/join", target); w.Code != http.StatusForbidden {
			t.Errorf("rejoin with %s: code=%d body=%s, want 403", code, w.Code, w.Body.String())
		}
	}

	w = doAsUser(t, router, http.MethodGet, "/v1/servers/"+srv.ID+"/bans", owner)
	var list struct {
		Bans []struct {
			UserID   string `json:"user_id"`
			BannedBy string `json:"banned_by"`
		} `json:"bans"`
		Total int `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != http.StatusOK || list.Total != 1 || list.Bans[0].UserID != target || list.Bans[0].BannedBy != owner {
		t.Errorf("list bans: code=%d body=%s", w.Code, w.Body.String())
	}

	// #26 CA1: revoking the ban publishes servers.member_unbanned, and the
	// user can rejoin with an invitation (they are not made a member by the
	// unban itself).
	if w = doAsUser(t, router, http.MethodDelete, "/v1/servers/"+srv.ID+"/bans/"+target, owner); w.Code != http.StatusNoContent {
		t.Fatalf("unban: code=%d body=%s", w.Code, w.Body.String())
	}
	unbanned := nextDeliveries(t, deliveries, 1)[moderationevents.RoutingKeyMemberUnbanned]
	var unbannedEvt moderationevents.MemberUnbanned
	_ = json.Unmarshal(unbanned.Body, &unbannedEvt)
	if unbannedEvt.ServerID != srv.ID || unbannedEvt.UserID != target || unbannedEvt.UnbannedBy != owner || unbannedEvt.EventID == "" {
		t.Errorf("member_unbanned = %+v (body %s)", unbannedEvt, unbanned.Body)
	}

	if w = doAsUser(t, router, http.MethodDelete, "/v1/servers/"+srv.ID+"/bans/"+target, owner); w.Code != http.StatusNotFound {
		t.Errorf("second unban: code=%d body=%s, want 404", w.Code, w.Body.String())
	}

	if w = doAsUser(t, router, http.MethodPost, "/v1/invites/"+inv.Code+"/join", target); w.Code != http.StatusCreated {
		t.Fatalf("rejoin after unban: code=%d body=%s", w.Code, w.Body.String())
	}
	if evt := waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined); evt.ServerID != srv.ID || evt.UserID != target {
		t.Errorf("rejoin event = %+v", evt)
	}
}

// #59 CA1, end to end over HTTP: the owner kicks a member, both
// servers.member_left and servers.member_kicked reach the discordia.events
// exchange, and -- unlike a ban -- the kicked user can rejoin right away with
// the same invitation.
func TestModerationEvents_KickEndToEnd(t *testing.T) {
	router, deliveries := setupE2E(t)

	owner := "owner-" + uuid.NewString()
	target := "target-" + uuid.NewString()

	w := postCreateServer(t, router, owner, "E2E Kick Server")
	if w.Code != http.StatusCreated {
		t.Fatalf("create server: code=%d body=%s", w.Code, w.Body.String())
	}
	var srv struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &srv)
	waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined) // owner

	w = doAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/invites", owner)
	var inv struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &inv)
	if w = doAsUser(t, router, http.MethodPost, "/v1/invites/"+inv.Code+"/join", target); w.Code != http.StatusCreated {
		t.Fatalf("join: code=%d body=%s", w.Code, w.Body.String())
	}
	waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined) // target

	// A plain member can't kick (CA3).
	if w = doJSONAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/kicks", target, `{"user_id":"`+owner+`"}`); w.Code != http.StatusForbidden {
		t.Fatalf("kick by a plain member: code=%d body=%s, want 403", w.Code, w.Body.String())
	}

	if w = doJSONAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/kicks", owner, `{"user_id":"`+target+`"}`); w.Code != http.StatusNoContent {
		t.Fatalf("kick: code=%d body=%s", w.Code, w.Body.String())
	}

	got := nextDeliveries(t, deliveries, 2)
	left, ok := got[membership.RoutingKeyMemberLeft]
	if !ok {
		t.Fatalf("no member_left event, got %v", keys(got))
	}
	var leftEvt membership.MemberLeft
	_ = json.Unmarshal(left.Body, &leftEvt)
	if leftEvt.ServerID != srv.ID || leftEvt.UserID != target {
		t.Errorf("member_left = %+v", leftEvt)
	}
	kicked, ok := got[moderationevents.RoutingKeyMemberKicked]
	if !ok {
		t.Fatalf("no member_kicked event, got %v", keys(got))
	}
	var kickedEvt moderationevents.MemberKicked
	_ = json.Unmarshal(kicked.Body, &kickedEvt)
	if kickedEvt.ServerID != srv.ID || kickedEvt.UserID != target || kickedEvt.KickedBy != owner || kickedEvt.EventID == "" {
		t.Errorf("member_kicked = %+v", kickedEvt)
	}

	// Not a member anymore: kicking again is a 404, and nothing was banned.
	if w = doJSONAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/kicks", owner, `{"user_id":"`+target+`"}`); w.Code != http.StatusNotFound {
		t.Errorf("second kick: code=%d body=%s, want 404", w.Code, w.Body.String())
	}

	if w = doAsUser(t, router, http.MethodPost, "/v1/invites/"+inv.Code+"/join", target); w.Code != http.StatusCreated {
		t.Fatalf("rejoin after kick: code=%d body=%s", w.Code, w.Body.String())
	}
	if evt := waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined); evt.ServerID != srv.ID || evt.UserID != target {
		t.Errorf("rejoin event = %+v", evt)
	}
}

// HU "Silenciar temporalmente a un miembro", CA1 and CA3: real HTTP requests
// persist the moderation action and publish the corresponding events through
// the outbox and RabbitMQ exchange.
func TestModerationEvents_MuteAndUnmuteEndToEnd(t *testing.T) {
	router, deliveries := setupE2E(t)

	owner := "owner-" + uuid.NewString()
	target := "target-" + uuid.NewString()

	w := postCreateServer(t, router, owner, "E2E Mute Server")
	if w.Code != http.StatusCreated {
		t.Fatalf("create server: code=%d body=%s", w.Code, w.Body.String())
	}
	var srv struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &srv); err != nil {
		t.Fatalf("decode server: %v", err)
	}
	waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined)

	w = doAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/invites", owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create invite: code=%d body=%s", w.Code, w.Body.String())
	}
	var invite struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &invite); err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	if w = doAsUser(t, router, http.MethodPost, "/v1/invites/"+invite.Code+"/join", target); w.Code != http.StatusCreated {
		t.Fatalf("join: code=%d body=%s", w.Code, w.Body.String())
	}
	waitForEvent(t, deliveries, membership.RoutingKeyMemberJoined)

	until := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	w = doJSONAsUser(t, router, http.MethodPost, "/v1/servers/"+srv.ID+"/mutes", owner,
		`{"user_id":"`+target+`","until_at":"`+until+`","reason":"spam"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("mute: code=%d body=%s", w.Code, w.Body.String())
	}

	muted := nextDeliveries(t, deliveries, 1)[moderationevents.RoutingKeyMemberMuted]
	var mutedEvent moderationevents.MemberMuted
	if err := json.Unmarshal(muted.Body, &mutedEvent); err != nil {
		t.Fatal(err)
	}
	if mutedEvent.ServerID != srv.ID || mutedEvent.UserID != target || mutedEvent.MutedBy != owner || mutedEvent.Reason != "spam" || mutedEvent.UntilAt.IsZero() || mutedEvent.EventID == "" {
		t.Errorf("member_muted = %+v", mutedEvent)
	}

	if w = doAsUser(t, router, http.MethodDelete, "/v1/servers/"+srv.ID+"/mutes/"+target, owner); w.Code != http.StatusNoContent {
		t.Fatalf("unmute: code=%d body=%s", w.Code, w.Body.String())
	}
	unmuted := nextDeliveries(t, deliveries, 1)[moderationevents.RoutingKeyMemberUnmuted]
	var unmutedEvent moderationevents.MemberUnmuted
	if err := json.Unmarshal(unmuted.Body, &unmutedEvent); err != nil {
		t.Fatal(err)
	}
	if unmutedEvent.ServerID != srv.ID || unmutedEvent.UserID != target || unmutedEvent.UnmutedBy != owner || unmutedEvent.Reason != "manual" || unmutedEvent.EventID == "" {
		t.Errorf("member_unmuted = %+v", unmutedEvent)
	}
}

func keys(m map[string]amqp.Delivery) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
