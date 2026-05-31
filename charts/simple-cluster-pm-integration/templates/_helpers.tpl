{{- define "simple-cluster-pm-integration.labels" -}}
app.kubernetes.io/name: simple-cluster-pm-integration
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: simple-cluster
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}
