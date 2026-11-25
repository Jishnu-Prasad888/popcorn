package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Admins manage accounts: a second bootstrap is refused, a new user can be
// disabled (and then can't sign in), have a new password set, and be
// promoted; the last admin can't lock themselves out.
func TestAdminManagesUsers(t *testing.T) {
	h, admin := newAuthTestServer(t)
	bootstrap(t, admin)

	// Sign-up is closed once an account exists.
	other := &testClient{handler: h}
	if rec := other.roundTrip(t, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"username":"mallory","password":"correct-horse-battery"}`); rec.Code != http.StatusConflict {
		t.Fatalf("second bootstrap = %d, want 409", rec.Code)
	}

	rec := admin.roundTrip(t, http.MethodPost, "/api/v1/users", `{"username":"guest","password":"guest-password-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		User struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.User.ID
	if !created.User.Enabled {
		t.Fatalf("new user should be enabled")
	}

	login := func(password string) int {
		c := &testClient{handler: h}
		return c.roundTrip(t, http.MethodPost, "/api/v1/auth/login", `{"username":"guest","password":"`+password+`"}`).Code
	}
	if code := login("guest-password-1"); code != http.StatusOK {
		t.Fatalf("guest login = %d", code)
	}

	if rec := admin.roundTrip(t, http.MethodPatch, "/api/v1/users/"+id, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body.String())
	}
	if code := login("guest-password-1"); code == http.StatusOK {
		t.Fatalf("disabled user could sign in")
	}

	if rec := admin.roundTrip(t, http.MethodPatch, "/api/v1/users/"+id, `{"enabled":true,"password":"another-password-2"}`); rec.Code != http.StatusOK {
		t.Fatalf("enable + password = %d %s", rec.Code, rec.Body.String())
	}
	if code := login("guest-password-1"); code == http.StatusOK {
		t.Fatalf("old password still works")
	}
	if code := login("another-password-2"); code != http.StatusOK {
		t.Fatalf("new password login = %d", code)
	}

	if rec := admin.roundTrip(t, http.MethodPatch, "/api/v1/users/"+id, `{"password":"short"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password = %d, want 400", rec.Code)
	}

	// The admin can't demote or disable themselves.
	var me struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(admin.roundTrip(t, http.MethodGet, "/api/v1/auth/me", "").Body.Bytes(), &me)
	if rec := admin.roundTrip(t, http.MethodPatch, "/api/v1/users/"+me.User.ID, `{"enabled":false}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("self-disable = %d, want 400", rec.Code)
	}

	// With a second admin, the first can be demoted by them; the last can't.
	if rec := admin.roundTrip(t, http.MethodPatch, "/api/v1/users/"+id, `{"role":"admin"}`); rec.Code != http.StatusOK {
		t.Fatalf("promote = %d %s", rec.Code, rec.Body.String())
	}
	if rec := admin.roundTrip(t, http.MethodPatch, "/api/v1/users/"+id, `{"role":"user"}`); rec.Code != http.StatusOK {
		t.Fatalf("demote other admin = %d %s", rec.Code, rec.Body.String())
	}

	// Members can't manage accounts.
	guest := &testClient{handler: h}
	guest.roundTrip(t, http.MethodPost, "/api/v1/auth/login", `{"username":"guest","password":"another-password-2"}`)
	if rec := guest.roundTrip(t, http.MethodPatch, "/api/v1/users/"+me.User.ID, `{"enabled":false}`); rec.Code != http.StatusForbidden {
		t.Fatalf("member patch = %d, want 403", rec.Code)
	}
}
