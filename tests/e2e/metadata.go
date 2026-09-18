package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReadSecretMetadata negotiates metadata exclusively. In particular it does
// not use client-go metadata's application/json full-object fallback.
func ReadSecretMetadata(ctx context.Context, client *http.Client, origin, namespace, name string) (*metav1.PartialObjectMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/v1/namespaces/"+url.PathEscape(namespace)+"/secrets/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, errors.New("metadata request invalid")
	}
	req.Header.Set("Accept", "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1")
	// Even a supplied authenticated client must not forward credentials on redirects.
	isolated := *client
	isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := isolated.Do(req)
	if err != nil {
		return nil, errors.New("metadata request failed")
	}
	defer resp.Body.Close()
	media, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || resp.StatusCode != http.StatusOK || media != "application/json" || params["as"] != "PartialObjectMetadata" || params["g"] != "meta.k8s.io" || params["v"] != "v1" {
		return nil, errors.New("strict metadata representation unavailable")
	}
	var obj metav1.PartialObjectMetadata
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&obj) != nil || obj.APIVersion != "meta.k8s.io/v1" || obj.Kind != "PartialObjectMetadata" || obj.Name != name || obj.Namespace != namespace || obj.UID == "" {
		return nil, errors.New("metadata response invalid")
	}
	return &obj, nil
}
