{{- define "datiprovider-ui.labels" -}}
app.kubernetes.io/name: datiprovider-ui
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: datiprovider
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}

{{- define "datiprovider-ui.image" -}}
{{- $tag := default .Chart.AppVersion .Values.ui.image.tag -}}
{{- printf "%s:%s" .Values.ui.image.repository $tag -}}
{{- end -}}

{{- define "datiprovider-ui.externalUrl" -}}
{{- printf "%s://%s" .Values.PUBLIC_SCHEME .Values.PUBLIC_HOST -}}
{{- end -}}
