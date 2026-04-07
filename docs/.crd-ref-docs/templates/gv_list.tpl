{{- define "gvList" -}}
{{- $groupVersions := . -}}
---
title: API Reference
outline: [2, 4]
---

# API Reference

## Packages

| Group / Version | Description |
| --- | --- |
{{- range $groupVersions }}
| {{ markdownRenderGVLink . }} | {{ if .Doc }}{{ .Doc | replace "\n" " " | trimSuffix "\n" | trunc 120 }}{{ else }}&mdash;{{ end }} |
{{- end }}

{{ range $groupVersions }}
{{ template "gvDetails" . }}
{{ end }}

{{- end -}}
