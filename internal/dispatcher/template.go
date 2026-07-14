package dispatcher

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed templates/*.tmpl
var templates embed.FS

func Render(name string, n Notification) (string, error) {
	tmpl, err := template.ParseFS(templates, "templates/"+name+".tmpl")
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, n); err != nil {
		return "", err
	}
	return buf.String(), nil
}
