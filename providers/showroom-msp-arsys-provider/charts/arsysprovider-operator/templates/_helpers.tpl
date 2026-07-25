{{- define "arsysprovider-operator.labels" -}}
app.kubernetes.io/name: arsysprovider-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: arsysprovider-operator
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}
