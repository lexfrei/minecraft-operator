{{/*
Expand the name of the chart.
*/}}
{{- define "minecraft-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 39 characters (not 63) because the longest resource
suffix is "-leader-election-binding" (24 chars), and Kubernetes names
must not exceed 63 characters total: 39 + 24 = 63.
*/}}
{{- define "minecraft-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 39 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 39 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 39 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "minecraft-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "minecraft-operator.labels" -}}
helm.sh/chart: {{ include "minecraft-operator.chart" . }}
{{ include "minecraft-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "minecraft-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "minecraft-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
