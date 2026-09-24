package manifest_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const approvedProductionImage = "ghcr.io/slqzeer/homelab-portal:sha-55659f8237fa5a2d0e5b79ab57268c011dc2dea1-36017826392-2@sha256:a566f89422953f2d6365e124415cb29d7eb350ac2ccf4f176fbedbb0f78100ef"

var immutableProductionImagePattern = regexp.MustCompile(`^ghcr\.io/slqzeer/homelab-portal:sha-[a-f0-9]{40}-[1-9][0-9]*-[1-9][0-9]*@sha256:[a-f0-9]{64}$`)

func immutableProductionImage(ref string) bool {
	return immutableProductionImagePattern.MatchString(ref) &&
		!strings.HasSuffix(ref, "sha256:"+strings.Repeat("0", 64))
}

func TestProductionImageReferenceRequiresUniqueTagAndNonzeroDigest(t *testing.T) {
	for _, tc := range []struct {
		name  string
		image string
		valid bool
	}{
		{"approved", approvedProductionImage, true},
		{"zero digest", strings.Replace(approvedProductionImage, "a566f89422953f2d6365e124415cb29d7eb350ac2ccf4f176fbedbb0f78100ef", strings.Repeat("0", 64), 1), false},
		{"tag only", strings.Split(approvedProductionImage, "@")[0], false},
		{"floating tag", "ghcr.io/slqzeer/homelab-portal:latest", false},
		{"floating tag with digest", strings.Replace(approvedProductionImage, ":sha-55659f8237fa5a2d0e5b79ab57268c011dc2dea1-36017826392-2@", ":latest@", 1), false},
		{"digest without unique tag", strings.Replace(approvedProductionImage, ":sha-55659f8237fa5a2d0e5b79ab57268c011dc2dea1-36017826392-2", "", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.valid, immutableProductionImage(tc.image))
		})
	}
}
