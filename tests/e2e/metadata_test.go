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

func TestSecretMetadataRefusesFullObjectBeforeReadingBody(t *testing.T) {
	const metadataType = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1"
	for _, tc := range []struct {
		name, media, body string
		status            int
		ok, unread        bool
	}{
		{"partial", metadataType, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"session","namespace":"portal","uid":"secret-uid"}}`, 200, true, false},
		{"full Secret fallback", "application/json", `{"kind":"Secret","data":{"password":"MUST_NOT_READ"}}`, 200, false, true},
		{"negotiation refused", "application/json", `{"message":"MUST_NOT_READ"}`, 406, false, true},
		{"wrong version", "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1beta1", `MUST_NOT_READ`, 200, false, true},
		{"lying content type", metadataType, `{"apiVersion":"v1","kind":"Secret","data":{"password":"not-metadata"}}`, 200, false, false},
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
			_, err := e2e.ReadSecretMetadata(context.Background(), client, "https://cluster.invalid", "portal", "session")
			if (err == nil) != tc.ok {
				t.Fatal("unexpected strict metadata result")
			}
			if tc.unread && body.reads != 0 {
				t.Fatal("full Secret/error body was read")
			}
		})
	}
}
