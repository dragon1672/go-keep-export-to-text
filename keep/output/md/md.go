package md

import (
	"strings"

	"path/filepath"
	"text/template"

	"github.com/dragon1672/go-keep-export-to-text/keep"
	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
)

// Writer writes the note to a markdown file.
type Writer struct {
	Writer    *keep.FileWriter
	Generator *keep.FileNameGenerator
	OutDir    string
}

func (w *Writer) note2Md(n *loader.Note) (string, error) {
	tmpl, err := template.New("text_file").Parse(`
{{- define "ListCheck"}}[{{if .IsChecked}}x{{else}} {{end}}]{{end -}}
{{- define "ListEntry"}} - {{template "ListCheck" .}} {{.Text}}{{end -}}
{{- /* start of file */ -}}
# {{.Title}}{{- with .CreatedMicros}} - [[{{.}}]]
Created: [[{{.}}]]{{end}}
{{- with .EditedMicros}}
Last Edited: {{.}}{{end}}

{{with .TextContent}}{{.}}
{{end}}
{{- with .ListContent}}{{range .}}{{template "ListEntry" .}}
{{end}}
{{- end}}{{- /* end of body */}} 

{{- with .Labels}}
{{range .}}#{{.Name}}
{{end}}{{end}}
{{- /* end of file */ -}}
`)
	if err != nil {
		return "", err
	}

	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, n); err != nil {
		return "", err
	}
	return sb.String(), nil
}
func (w *Writer) WriteNote(n *loader.Note) error {
	fileName := w.Generator.GenerateAndReserve(n)
	filePath, err := filepath.Abs(filepath.Join(w.OutDir, fileName+".md"))
	if err != nil {
		return err
	}
	md, err := w.note2Md(n)
	if err != nil {
		return err
	}
	return w.Writer.WriteFile(md, filePath)
}

func (w *Writer) Flush() error {return nil }

var _ keep.NoteWriter = (*Writer)(nil)