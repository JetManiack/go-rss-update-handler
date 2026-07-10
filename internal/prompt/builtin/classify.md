---
name: classify
version: "1.0.0"
critical: true
description: "Classify an update against the feed's recent important updates"
---
Оцени важность обновления проекта.

## Текущее обновление
{{ .Current.RawContent }}

## Последние важные обновления (для сравнения)
{{ range .History }}- {{ .Event.PublishedAt }}: {{ .Verdict.Reason }}
{{ else }}Истории важных обновлений нет.
{{ end }}

Ответь строго JSON: {"important": bool, "category": string, "confidence": float, "reason": string}
