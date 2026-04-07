{{- define "type" -}}
{{- $type := . -}}
{{- if markdownShouldRenderType $type -}}

<div class="crd-type">

### {{ $type.Name }}{{ if $type.GVK }} <Badge type="info" text="Kind" />{{ end }}

{{ $type.Doc | replace "\n\n" "<<PARA>>" | replace "\n" " " | replace "<<PARA>>" "\n\n" }}

{{ if or $type.References $type.IsAlias -}}
<small>
{{- if $type.References -}}
<strong>Appears in:</strong>{{- range $i, $ref := $type.SortedReferences }}{{ if $i }},{{ end }} {{ markdownRenderTypeLink $ref }}{{ end }}
{{- end -}}
{{- if and $type.References $type.IsAlias }}<br>{{ end -}}
{{- if $type.IsAlias -}}
<strong>Underlying type:</strong> _{{ markdownRenderType $type.UnderlyingType }}_
{{- end -}}
</small>
{{ end }}

{{ if $type.Members -}}
| Field | Description | Default | Validation |
| --- | --- | --- | --- |
{{ if $type.GVK -}}
| `apiVersion` _string_ | `{{ $type.GVK.Group }}/{{ $type.GVK.Version }}` | | |
| `kind` _string_ | `{{ $type.GVK.Kind }}` | | |
{{ end -}}
{{ range $type.Members -}}
| `{{ .Name }}` _{{ markdownRenderType .Type }}_ | {{ template "type_members" . }} | {{- $default := markdownRenderDefault .Default -}}{{ if $default }}`{{ $default }}`{{ end }} | {{ template "type_validation" . }} |
{{ end -}}
{{ end -}}

{{ if $type.EnumValues -}}
| Value | Description |
| --- | --- |
{{ range $type.EnumValues -}}
| `{{ .Name }}` | {{ markdownRenderFieldDoc (.Doc | replace "\n\n" "<<PARA>>" | replace "\n" " " | replace "<<PARA>>" "\n\n") }} |
{{ end -}}
{{ end -}}

{{ if $type.Validation -}}
_Validation:_
{{- range $type.Validation }}
- {{ . }}
{{- end }}
{{ end }}

</div>

{{- end -}}
{{- end -}}

{{- define "type_validation" -}}
{{- range .Validation -}}
{{- if hasPrefix "Required:" . }} <Badge type="danger" text="Required" /><br>{{- end -}}
{{- if hasPrefix "Optional:" . }} <Badge type="tip" text="Optional" /><br>{{- end -}}
{{- end -}}
{{- range .Validation -}}
{{- if not (or (hasPrefix "Required:" .) (hasPrefix "Optional:" .)) -}}
{{ markdownRenderFieldDoc . }}<br>
{{- end -}}
{{- end -}}
{{- end -}}
