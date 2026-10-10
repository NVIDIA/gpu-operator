{{/*
Expand the name of the chart.
*/}}
{{- define "gpu-operator-certification.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "gpu-operator-certification.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s" $name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "gpu-operator-certification.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "gpu-operator-certification.labels" -}}
helm.sh/chart: {{ include "gpu-operator-certification.chart" . }}
{{ include "gpu-operator-certification.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "gpu-operator-certification.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gpu-operator-certification.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
