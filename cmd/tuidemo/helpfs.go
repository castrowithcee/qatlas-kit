package main

import (
	"embed"
	"io/fs"
)

//go:embed help
var helpFiles embed.FS

// helpPages is the Markdown help of the workbench, a topic per folder; see package helppages.
func helpPages() fs.FS {
	pages, err := fs.Sub(helpFiles, "help")
	if err != nil {
		panic(err) // the directory is embedded above, so it exists
	}
	return pages
}
