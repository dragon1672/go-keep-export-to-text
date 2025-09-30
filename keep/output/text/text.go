package text

import (
	"fmt"
	"strings"

	"path/filepath"
	"text/template"

	"github.com/dragon1672/go-keep-export-to-text/keep"
	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
)

type Writer struct {
	Writer    *keep.FileWriter
	Generator *keep.FileNameGenerator
	OutDir    string
}

func (w *Writer) Flush() error {return nil }

func note2Txt(n *loader.Note) (string, error) {
	title, subheader, body, err := Note2TxtParts(n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n%s\n\n%s", title, subheader, body), nil
}

func Note2TxtParts(n *loader.Note) (string, string, string, error) {
	title, err := note2TxtTitle(n)
	if err != nil {
		return "","","", err
	}
	subHeader, err := note2TxtSubHeader(n)
	if err != nil {
		return "","","", err
	}
	body, err := note2TxtBody(n)
	if err != nil {
		return "","","", err
	}
	return title, subHeader, body, nil
}

func note2TxtTitle(n *loader.Note) (string, error) {
	tmpl, err := template.New("text_file").Parse(`
{{- /* start of file */ -}}
{{- .Title}}
{{- /* end of file */ -}}
`)
	if err != nil {
		return "", err
	}

	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, n); err != nil {
		return "", err
	}
	return strings.Trim(sb.String(), "\n "), nil
}

func note2TxtSubHeader(n *loader.Note) (string, error) {
	tmpl, err := template.New("text_file").Parse(`
{{- /* start of file */ -}}
{{- with .CreatedMicros}}
Created: {{.}}{{end}}
{{- with .EditedMicros}}
Edited: {{.}}{{end}}
{{- /* end of file */ -}}
`)
	if err != nil {
		return "", err
	}

	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, n); err != nil {
		return "", err
	}
	return strings.Trim(sb.String(), "\n "), nil
}

func note2TxtBody(n *loader.Note) (string, error) {
	tmpl, err := template.New("text_file").Parse(`
{{- define "ListCheck"}}[{{if .IsChecked}}x{{else}} {{end}}]{{end -}}
{{- define "ListEntry"}} - {{template "ListCheck" .}} {{.Text}}{{end -}}
{{- /* start of file */ -}}
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
	return strings.Trim(sb.String(), "\n "), nil
}
func (w *Writer) WriteNote(n *loader.Note) error {
	fileName := w.Generator.GenerateAndReserve(n)
	filePath, err := filepath.Abs(filepath.Join(w.OutDir, fileName+".txt"))
	if err != nil {
		return err
	}
	txt, err := note2Txt(n)
	if err != nil {
		return err
	}
	return w.Writer.WriteFile(txt, filePath)
}

var _ keep.NoteWriter = (*Writer)(nil)