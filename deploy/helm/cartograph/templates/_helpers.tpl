{{- define "cartograph.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cartograph.fullname" -}}
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

{{- define "cartograph.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cartograph.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cartograph.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "cartograph.labels" -}}
helm.sh/chart: {{ include "cartograph.chart" . }}
{{ include "cartograph.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "cartograph.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "cartograph.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "cartograph.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{/* Whether a Postgres store is configured, as "true" or "". */}}
{{- define "cartograph.hasStore" -}}
{{- if or .Values.store.url .Values.store.existingSecret }}true{{ end }}
{{- end }}

{{/* The Secret holding the store URLs. */}}
{{- define "cartograph.storeSecretName" -}}
{{- default (printf "%s-store" (include "cartograph.fullname" .)) .Values.store.existingSecret }}
{{- end }}

{{/* Whether a fan-out URL is configured, as "true" or "". */}}
{{- define "cartograph.hasFanoutUrl" -}}
{{- if .Values.store.existingSecret }}{{ if .Values.store.fanoutUrlKey }}true{{ end }}{{ else if .Values.store.fanoutUrl }}true{{ end }}
{{- end }}

{{- define "cartograph.terminationGracePeriodSeconds" -}}
{{- $floor := add .Values.config.drainDelaySeconds .Values.config.shutdownTimeoutSeconds }}
{{- if kindIs "invalid" .Values.terminationGracePeriodSeconds }}
{{- add $floor 10 }}
{{- else }}
{{- .Values.terminationGracePeriodSeconds }}
{{- end }}
{{- end }}

{{/*
Refuse values that would deploy something broken. Every template
includes this, so `helm template` and `helm install` fail before
anything is applied.
*/}}
{{- define "cartograph.validate" -}}
{{- $store := include "cartograph.hasStore" . }}
{{- $scaled := or .Values.autoscaling.enabled .Values.keda.enabled }}
{{- if and .Values.autoscaling.enabled .Values.keda.enabled }}
{{- fail "autoscaling.enabled and keda.enabled are both set: choose one autoscaler (a KEDA ScaledObject makes its own HorizontalPodAutoscaler)" }}
{{- end }}
{{- if and .Values.vault.enabled $store }}
{{- fail "vault.enabled and a store are both set: a Postgres store replaces the vault; choose one" }}
{{- end }}
{{- if and (not $store) (or (gt (int .Values.replicaCount) 1) $scaled) }}
{{- fail "more than one replica, or an autoscaler, needs a Postgres store: a vault cannot be shared between replicas. Set store.url (or store.existingSecret), or run one replica on a volume with replicaCount=1 and vault.enabled=true" }}
{{- end }}
{{- if and (not $store) (not .Values.vault.enabled) }}
{{- fail "no state configured: set store.url (or store.existingSecret) for a Postgres store, or vault.enabled=true with replicaCount=1 for one replica on a volume" }}
{{- end }}
{{- if and .Values.keda.enabled (not .Values.keda.prometheus.serverAddress) }}
{{- fail "keda.enabled needs keda.prometheus.serverAddress, the Prometheus KEDA queries" }}
{{- end }}
{{- if and .Values.keda.enabled (not .Values.metrics.enabled) }}
{{- fail "keda.enabled scales on Cartograph's metrics: set metrics.enabled=true" }}
{{- end }}
{{- if and .Values.metrics.serviceMonitor.enabled (not .Values.metrics.enabled) }}
{{- fail "metrics.serviceMonitor.enabled needs metrics.enabled=true" }}
{{- end }}
{{- if and (eq .Values.authz.mode "roles") (ne .Values.auth.mode "proxy") }}
{{- fail "authz.mode=roles needs an authenticator: set auth.mode=proxy" }}
{{- end }}
{{- if and (eq .Values.store.fanout "postgres") (not $store) }}
{{- fail "store.fanout=postgres needs a Postgres store" }}
{{- end }}
{{- if not (kindIs "invalid" .Values.terminationGracePeriodSeconds) }}
{{- if le (int .Values.terminationGracePeriodSeconds) (int (add .Values.config.drainDelaySeconds .Values.config.shutdownTimeoutSeconds)) }}
{{- fail (printf "terminationGracePeriodSeconds (%v) must be greater than config.drainDelaySeconds + config.shutdownTimeoutSeconds (%v), or the kubelet kills a pod that is still draining" .Values.terminationGracePeriodSeconds (add .Values.config.drainDelaySeconds .Values.config.shutdownTimeoutSeconds)) }}
{{- end }}
{{- end }}
{{- end }}

{{/* Whether more than one pod may run: the PDB only means something then. */}}
{{- define "cartograph.multiReplica" -}}
{{- if or (gt (int .Values.replicaCount) 1) .Values.autoscaling.enabled .Values.keda.enabled }}true{{ end }}
{{- end }}
