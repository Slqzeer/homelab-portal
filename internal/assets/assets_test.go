package assets

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSContainsAccessibleLocalHomeShell(t *testing.T) {
	home, err := fs.ReadFile(FS(), "index.html")
	require.NoError(t, err)

	html := string(home)
	assert.Contains(t, html, `href="#main-content"`)
	assert.Contains(t, html, `<main id="main-content"`)
	assert.Contains(t, html, `href="/_astro/`)
	assert.Contains(t, html, `src="/app.js"`)
	assert.NotContains(t, strings.ToLower(html), "https://")
	assert.NotContains(t, strings.ToLower(html), "http://")
}
