{{/*
Expand the name of the chart.
*/}}
{{- define "kubewa-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "kubewa-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart label.
*/}}
{{- define "kubewa-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "kubewa-operator.labels" -}}
helm.sh/chart: {{ include "kubewa-operator.chart" . }}
{{ include "kubewa-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "kubewa-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubewa-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "kubewa-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kubewa-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Twilio secret name.
*/}}
{{- define "kubewa-operator.twilioSecretName" -}}
{{- if .Values.twilio.existingSecret }}
{{- .Values.twilio.existingSecret }}
{{- else }}
{{- include "kubewa-operator.fullname" . }}-twilio-secret
{{- end }}
{{- end }}
