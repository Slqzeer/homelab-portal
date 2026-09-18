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
	if err != nil || resp.StatusCode != http.StatusOK || media != "application/json" || !metadataMediaParameters(params) {
		return nil, errors.New("strict metadata representation unavailable")
	}
	const maxMetadataBytes = 128 << 10
	limited := &io.LimitedReader{R: resp.Body, N: maxMetadataBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var obj metav1.PartialObjectMetadata
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("metadata response invalid")
	}
	seen := make(map[string]bool, 3)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return nil, errors.New("metadata response invalid")
		}
		seen[key] = true
		switch key {
		case "apiVersion":
			err = decoder.Decode(&obj.APIVersion)
		case "kind":
			err = decoder.Decode(&obj.Kind)
		case "metadata":
			err = decoder.Decode(&obj.ObjectMeta)
		default:
			// Reject full-object fields without decoding, retaining, or logging values.
			return nil, errors.New("metadata response invalid")
		}
		if err != nil {
			return nil, errors.New("metadata response invalid")
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errors.New("metadata response invalid")
	}
	if !onlyJSONWhitespace(io.MultiReader(decoder.Buffered(), limited)) || limited.N == 0 || len(seen) != 3 || obj.APIVersion != "meta.k8s.io/v1" || obj.Kind != "PartialObjectMetadata" || obj.Name != name || obj.Namespace != namespace || obj.UID == "" {
		return nil, errors.New("metadata response invalid")
	}
	return &obj, nil
}

func onlyJSONWhitespace(r io.Reader) bool {
	var buffer [4096]byte
	for {
		n, err := r.Read(buffer[:])
		for _, b := range buffer[:n] {
			if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
				return false
			}
		}
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
	}
}

func metadataMediaParameters(params map[string]string) bool {
	_, hasAs := params["as"]
	_, hasGroup := params["g"]
	_, hasVersion := params["v"]
	if !hasAs && !hasGroup && !hasVersion {
		return true
	}
	return params["as"] == "PartialObjectMetadata" && params["g"] == "meta.k8s.io" && params["v"] == "v1"
}
