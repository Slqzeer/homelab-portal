package e2e_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	e2e "github.com/Slqzeer/homelab-portal/tests/e2e"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type readDetector struct {
	io.Reader
	reads int
}

func (r *readDetector) Read(p []byte) (int, error) { r.reads++; return r.Reader.Read(p) }
func (r *readDetector) Close() error               { return nil }

func TestSecretMetadataNegotiationAndShape(t *testing.T) {
	const metadataType = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1"
	const partial = `{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1","metadata":{"name":"session","namespace":"portal","uid":"secret-uid","resourceVersion":"12842","creationTimestamp":"2026-09-18T12:00:00Z","labels":{"app.kubernetes.io/managed-by":"vault-secrets-operator"}}}`
	for _, tc := range []struct {
		name, media, body string
		status            int
		ok, unread        bool
	}{
		{"upstream plain JSON metadata", "application/json", partial, 200, true, false},
		{"negotiated metadata media type", metadataType, partial, 200, true, false},
		{"full Secret data", "application/json", `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"session","namespace":"portal","uid":"secret-uid"},"data":{"password":"MUST_NOT_EXPOSE"}}`, 200, false, false},
		{"full Secret stringData", "application/json", `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"session","namespace":"portal","uid":"secret-uid"},"stringData":{"password":"MUST_NOT_EXPOSE"}}`, 200, false, false},
		{"full Secret type", "application/json", `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"session","namespace":"portal","uid":"secret-uid"},"type":"MUST_NOT_EXPOSE"}`, 200, false, false},
		{"unknown top-level field", "application/json", `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"session","namespace":"portal","uid":"secret-uid"},"unexpected":"MUST_NOT_EXPOSE"}`, 200, false, false},
		{"trailing object", "application/json", partial + ` {"data":{"password":"MUST_NOT_EXPOSE"}}`, 200, false, false},
		{"oversized metadata", "application/json", `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"session","namespace":"portal","uid":"secret-uid","annotations":{"large":"` + strings.Repeat("x", 128<<10) + `"}}}`, 200, false, false},
		{"negotiation refused", "application/json", `{"message":"MUST_NOT_READ"}`, 406, false, true},
		{"wrong version", "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1beta1", `MUST_NOT_READ`, 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &readDetector{Reader: strings.NewReader(tc.body)}
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Accept") != metadataType {
					t.Error("request permits full-object negotiation fallback")
				}
				if req.URL.Path != "/api/v1/namespaces/portal/secrets/session" {
					t.Error("wrong metadata endpoint")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.media}}, Body: body}, nil
			})}
			obj, err := e2e.ReadSecretMetadata(context.Background(), client, "https://cluster.invalid", "portal", "session")
			if (err == nil) != tc.ok {
				t.Fatal("unexpected strict metadata result")
			}
			if err != nil && strings.Contains(err.Error(), "MUST_NOT_EXPOSE") {
				t.Fatal("Secret value escaped through metadata error")
			}
			if tc.ok && (obj.Name != "session" || obj.Namespace != "portal" || obj.UID != "secret-uid" || obj.ResourceVersion != "12842") {
				t.Fatal("accepted metadata identity differs")
			}
			if tc.unread && body.reads != 0 {
				t.Fatal("rejected response body was read")
			}
		})
	}
}
