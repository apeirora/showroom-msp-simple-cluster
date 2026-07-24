{{- define "sapprovider-operator.labels" -}}
app.kubernetes.io/name: sapprovider-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: sapprovider-operator
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}
