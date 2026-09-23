package manifest_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const approvedProductionImage = "ghcr.io/slqzeer/homelab-portal:sha-5fa18953191ebd1e0b9482a72f649a18900f77b4-35928856825-1@sha256:b6a815c29106a88472313239d621aa851ec795d8713949454934982685b8b663"

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
		{"zero digest", strings.Replace(approvedProductionImage, "b6a815c29106a88472313239d621aa851ec795d8713949454934982685b8b663", strings.Repeat("0", 64), 1), false},
		{"tag only", strings.Split(approvedProductionImage, "@")[0], false},
		{"floating tag", "ghcr.io/slqzeer/homelab-portal:latest", false},
		{"floating tag with digest", strings.Replace(approvedProductionImage, ":sha-5fa18953191ebd1e0b9482a72f649a18900f77b4-35928856825-1@", ":latest@", 1), false},
		{"digest without unique tag", strings.Replace(approvedProductionImage, ":sha-5fa18953191ebd1e0b9482a72f649a18900f77b4-35928856825-1", "", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.valid, immutableProductionImage(tc.image))
		})
	}
}
