package portalhttp_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Slqzeer/homelab-portal/internal/auth"
)

func TestHomeProfileEntrySeparatesAnonymousAndAuthenticatedIdentity(t *testing.T) {
	f := newFixture(t)
	anonymous := f.request(http.MethodGet, "/", nil).Body.String()
	for _, want := range []string{`href="/auth/login"`, ">Sign in<"} {
		if !strings.Contains(anonymous, want) {
			t.Errorf("anonymous home missing %q", want)
		}
	}
	for _, forbidden := range []string{`href="/profile"`, "profile-preview", "Signed-in user", "Ada Lovelace"} {
		if strings.Contains(anonymous, forbidden) {
			t.Errorf("anonymous home exposed %q", forbidden)
		}
	}

	cookie := f.cookieWithClaims(t, auth.Claims{DisplayName: `<Ada Lovelace>`})
	authenticated := f.request(http.MethodGet, "/", cookie).Body.String()
	for _, want := range []string{`href="/profile"`, `aria-describedby="profile-preview"`, "&lt;L", "Signed in.", "&lt;Ada"} {
		if !strings.Contains(authenticated, want) {
			t.Errorf("authenticated home missing %q", want)
		}
	}
	for _, forbidden := range []string{`>Sign in<`, `<Ada Lovelace>`, "private-subject", "private-access-token", "private-refresh-token", "portal-admin"} {
		if strings.Contains(authenticated, forbidden) {
			t.Errorf("authenticated home leaked %q", forbidden)
		}
	}
	preview := regexp.MustCompile(`(?s)<span id="profile-preview"[^>]*>(.*?)</span>`).FindStringSubmatch(authenticated)
	if len(preview) != 2 {
		t.Fatalf("profile preview not found: %s", authenticated)
	}
	for _, interactive := range []string{"<a", "<button", "<input", "tabindex="} {
		if strings.Contains(preview[1], interactive) {
			t.Errorf("profile preview contains interactive descendant %q", interactive)
		}
	}

	fallback := f.request(http.MethodGet, "/", f.cookie(t)).Body.String()
	for _, want := range []string{`aria-label="? — Profile: Signed-in user"`, ">?</a>", "Signed-in user", "Signed in."} {
		if !strings.Contains(fallback, want) {
			t.Errorf("fallback home missing %q", want)
		}
	}
}

func TestProfileRendersOnlyApprovedAccountDataAndConditionalAdminLink(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name      string
		claims    auth.Claims
		wantLabel string
		wantAdmin bool
	}{
		{name: "named user", claims: auth.Claims{DisplayName: `<Ada Lovelace>`}, wantLabel: "&lt;Ada"},
		{name: "fallback user", claims: auth.Claims{}, wantLabel: "Signed-in user"},
		{name: "administrator", claims: auth.Claims{Groups: []string{"portal-admin"}, DisplayName: "Ada Lovelace"}, wantLabel: "Ada Lovelace", wantAdmin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.request(http.MethodGet, "/profile", f.cookieWithClaims(t, tc.claims))
			if w.Code != http.StatusOK {
				t.Fatalf("profile = %d: %s", w.Code, w.Body)
			}
			body := w.Body.String()
			for _, want := range []string{tc.wantLabel, "Signed in.", `name="csrf_token"`, "Sign out", `href="/"`, "Back to catalogue"} {
				if !strings.Contains(body, want) {
					t.Errorf("profile missing %q", want)
				}
			}
			if strings.Contains(body, `href="/admin"`) != tc.wantAdmin {
				t.Errorf("admin link presence = %v, want %v", strings.Contains(body, `href="/admin"`), tc.wantAdmin)
			}
			for _, forbidden := range []string{`<Ada Lovelace>`, "portal-admin", "private-subject", "private-access-token", "private-refresh-token", "email", "groups", "id_token"} {
				if strings.Contains(body, forbidden) {
					t.Errorf("profile leaked %q", forbidden)
				}
			}
		})
	}
}

func TestProfileRequiresAuthenticationAndIsAbsentFromOperationsListener(t *testing.T) {
	f := newFixture(t)
	if w := f.request(http.MethodGet, "/profile", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("anonymous profile without provider = %d", w.Code)
	}
	if w := f.operations("/profile"); w.Code != http.StatusNotFound {
		t.Fatalf("operations profile = %d", w.Code)
	}
}
