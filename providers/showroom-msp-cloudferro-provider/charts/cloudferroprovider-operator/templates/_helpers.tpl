{{- define "cloudferroprovider-operator.labels" -}}
app.kubernetes.io/name: cloudferroprovider-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: cloudferroprovider-operator
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}
