{{- define "docker-cache-server.image" -}}
{{- $image := (include "common.images.image" (dict "imageRoot" .Values.image "global" .Values.global)) }}
{{- if eq .Values.image.tag "" -}}
{{- $image = (printf "%s%s" $image .Chart.AppVersion) -}}
{{- end -}}
{{- $image -}}
{{- end -}}
{{- define "docker-cache-server.imagePullSecrets" -}}
{{- include "common.images.pullSecrets" (dict "images" (list .Values.image) "global" .Values.global) -}}
{{- end -}}
{{- define "docker-cache-server.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "common.names.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}
{{- define "docker-cache-server.configMapName" -}}
{{- if .Values.existingConfigMap -}}
{{- .Values.existingConfigMap -}}
{{- else -}}
{{ template "common.names.fullname" . }}-config
{{- end -}}
{{- end -}}

{{- define "docker-cache-server.resources.requests" }}
{{- $def := dict -}}
{{- if not .Values.cacheStorage.persistence.enabled -}}
{{- $def = dict "ephemeral-storage" .Values.cacheStorage.emptyDir.sizeLimit }}
{{- end -}}
{{- merge .Values.resources.requests $def | toYaml -}}
{{- end }}

{{- define "docker-cache-server.cacheStorage.pvcName" }}
{{- printf "%s-cache" (include "common.names.fullname" .) }}
{{- end }}

{{- define "docker-cache-server.configmapName" -}}
{{- printf "%s-config" (include "common.names.fullname" .) -}}
{{- end }}

