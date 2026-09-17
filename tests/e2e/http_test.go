package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	e2e "github.com/Slqzeer/homelab-portal/tests/e2e"
)

func TestCatalogAcceptanceChecksActualLinksAndDoesNotLeakResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"current HTTPS link", `<a href="https://current.test.ts.net">Public fixture</a>`, true},
		{"stale hostname", `<a href="https://old.test.ts.net">Public fixture</a>`, false},
		{"unpaired name and URL", `<a href="https://wrong.test.ts.net">Public fixture</a><a href="https://current.test.ts.net">Other</a>`, false},
		{"unauthorized name", `<a href="https://current.test.ts.net">Public fixture</a>Hidden fixture`, false},
		{"unauthorized URL", `<a href="https://current.test.ts.net">Public fixture</a>https://hidden.test.ts.net`, false},
		{"unexpected stale state", `<a href="https://current.test.ts.net">Public fixture</a><aside class="stale-banner">stale</aside>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cookie, err := r.Cookie("__Host-portal_session")
				if err != nil || cookie.Value != "secret-value" {
					t.Error("test identity was not sent as a cookie")
				}
				_, _ = w.Write([]byte(tc.body + " secret-value"))
			}))
			defer srv.Close()
			err := e2e.CheckCatalog(srv.Client(), srv.URL, "secret-value", []e2e.Item{{Name: "Public fixture", URL: "https://current.test.ts.net"}}, []e2e.Item{{Name: "Hidden fixture", URL: "https://hidden.test.ts.net"}}, false)
			if (err == nil) != tc.ok {
				t.Fatal("catalog acceptance result disagrees with HTTP behavior")
			}
			if err != nil && strings.Contains(err.Error(), "secret-value") {
				t.Fatal("response data leaked")
			}
		})
	}
}
