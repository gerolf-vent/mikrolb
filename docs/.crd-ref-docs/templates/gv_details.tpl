{{- define "gvDetails" -}}
{{- $gv := . -}}

## {{ $gv.GroupVersionString }}

{{ if $gv.Doc -}}
{{ $gv.Doc | replace "\n\n" "<<PARA>>" | replace "\n" " " | replace "<<PARA>>" "\n\n" }}
{{ end -}}

{{- if $gv.Kinds }}

<div class="details custom-block">
<p><b>Resource Types</b></p>
<ul>
{{- range $gv.SortedKinds }}
<li><a href="#{{ ($gv.TypeForKind .).Name | lower }}">{{ ($gv.TypeForKind .).Name }}</a></li>
{{- end }}
</ul>
</div>
{{ end }}

{{ range $gv.SortedTypes }}
{{ template "type" . }}
{{ end }}

{{- end -}}
