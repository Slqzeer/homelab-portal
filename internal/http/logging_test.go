package portalhttp_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogsContainOnlyGeneratedIDAndSafeEventResult(t *testing.T) {
	f := newFixture(t)
	r := httptest.NewRequest("GET", "https://portal.example/auth/callback?code=private-code&state=private-state&error_description=private-error", nil)
	r.Header.Set("Cookie", "__Host-portal_session=private-cookie; __Host-portal_login=private-transaction")
	r.Header.Set("Authorization", "Bearer private-token")
	r.Header.Set("X-Request-ID", "private-user-id")
	r.Header.Set("Referer", "https://private-target.example")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	var entry map[string]any
	if err := json.Unmarshal(f.logs.Bytes(), &entry); err != nil {
		t.Fatalf("missing JSON request log: %q: %v", f.logs.String(), err)
	}
	for key := range entry {
		switch key {
		case "time", "level", "msg", "request_id", "event", "result":
		default:
			t.Errorf("unapproved log field %s", key)
		}
	}
	if entry["event"] != "callback" || entry["result"] != "400" || entry["request_id"] == "" || entry["request_id"] != w.Header().Get("X-Request-ID") {
		t.Errorf("unsafe/missing event correlation: %v", entry)
	}
	if strings.Contains(f.logs.String(), "private-") {
		t.Errorf("request secrets leaked into logs: %s", f.logs.String())
	}
}
