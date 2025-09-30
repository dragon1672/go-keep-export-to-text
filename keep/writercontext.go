package keep

import (
	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
)

type NoteWriter interface {
	WriteNote(n *loader.Note) error
	Flush() error
}
