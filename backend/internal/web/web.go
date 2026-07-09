package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

func Dist() fs.FS {
	webFS, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return webFS
}
