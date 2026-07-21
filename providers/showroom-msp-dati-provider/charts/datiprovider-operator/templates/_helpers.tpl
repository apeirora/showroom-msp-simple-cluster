{{- define "datiprovider-operator.labels" -}}
app.kubernetes.io/name: datiprovider-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: datiprovider-operator
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}
