{{- define "arsysprovider-ui.labels" -}}
app.kubernetes.io/name: arsysprovider-ui
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: arsysprovider
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}

{{- define "arsysprovider-ui.image" -}}
{{- $tag := default .Chart.AppVersion .Values.ui.image.tag -}}
{{- printf "%s:%s" .Values.ui.image.repository $tag -}}
{{- end -}}

{{- define "arsysprovider-ui.externalUrl" -}}
{{- printf "%s://%s" .Values.PUBLIC_SCHEME .Values.PUBLIC_HOST -}}
{{- end -}}
