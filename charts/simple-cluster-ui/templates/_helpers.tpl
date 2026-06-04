{{- define "simple-cluster-ui.labels" -}}
app.kubernetes.io/name: simple-cluster-ui
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: simple-cluster
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}

{{- define "simple-cluster-ui.image" -}}
{{- $tag := default .Chart.AppVersion .Values.ui.image.tag -}}
{{- printf "%s:%s" .Values.ui.image.repository $tag -}}
{{- end -}}

{{- define "simple-cluster-ui.externalUrl" -}}
{{- printf "%s://%s" .Values.PUBLIC_SCHEME .Values.PUBLIC_HOST -}}
{{- end -}}
