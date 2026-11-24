{{- define "dinahosting-webhook.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dinahosting-webhook.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "dinahosting-webhook.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dinahosting-webhook.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "dinahosting-webhook.labels" -}}
{{ include "dinahosting-webhook.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "dinahosting-webhook.selfSignedIssuer" -}}
{{ printf "%s-selfsign" (include "dinahosting-webhook.fullname" .) }}
{{- end -}}

{{- define "dinahosting-webhook.rootCAIssuer" -}}
{{ printf "%s-ca" (include "dinahosting-webhook.fullname" .) }}
{{- end -}}

{{- define "dinahosting-webhook.rootCACertificate" -}}
{{ printf "%s-ca" (include "dinahosting-webhook.fullname" .) }}
{{- end -}}

{{- define "dinahosting-webhook.servingCertificate" -}}
{{ printf "%s-webhook-tls" (include "dinahosting-webhook.fullname" .) }}
{{- end -}}
