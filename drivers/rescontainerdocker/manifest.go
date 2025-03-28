package rescontainerdocker

import (
	"embed"

	"github.com/opensvc/om3/core/driver"
	"github.com/opensvc/om3/core/keywords"
	"github.com/opensvc/om3/core/manifest"
)

var (
	//go:embed text
	fs embed.FS
)

var (
	DrvID = driver.NewID(driver.GroupContainer, "docker")
)

func init() {
	driver.Register(DrvID, New)
}

// Manifest exposes to the core the input expected by the driver.
func (t *T) Manifest() *manifest.T {
	m := t.BT.ManifestWithID(DrvID)
	m.Add(
		keywords.Keyword{
			Option:   "userns",
			Attr:     "UserNS",
			Scopable: true,
			Example:  "container#0",
			Text:     keywords.NewText(fs, "text/kw/userns"),
		},
	)
	return m
}
