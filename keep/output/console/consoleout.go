package console

import (
	"fmt"

	"github.com/dragon1672/go-keep-export-to-text/keep"
	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
)

type StdOut struct {}

func (s *StdOut) Flush() error {return nil }

func (s *StdOut) WriteNote(n *loader.Note) error {
	fmt.Printf("```note\n%+v\n```\n", n)
	return nil
}

var _ keep.NoteWriter = (*StdOut)(nil)