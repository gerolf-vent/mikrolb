{{- define "type_members" -}}
{{- $field := . -}}
{{- if eq $field.Name "metadata" -}}
Refer to Kubernetes API documentation for fields of `metadata`.
{{- else -}}
{{ markdownRenderFieldDoc ($field.Doc | replace "\n\n" "<<PARA>>" | replace "\n" " " | replace "<<PARA>>" "\n\n") }}
{{- end -}}
{{- end -}}
