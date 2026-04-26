package discovery

import (
	"strings"

	"github.com/megawron/lok8s/types"
)

func MatchPod(pod *types.Pod, labelSelectorStr, fieldSelectorStr string) bool {
	if !MatchLabels(pod.Metadata.Labels, labelSelectorStr) {
		return false
	}
	if !MatchFields(pod, fieldSelectorStr) {
		return false
	}
	return true
}

func MatchLabels(labels map[string]string, selector string) bool {
	if selector == "" {
		return true
	}
	parts := strings.Split(selector, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "!=") {
			kv := strings.Split(part, "!=")
			if len(kv) != 2 {
				return false
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if labels[k] == v {
				return false
			}
		} else if strings.Contains(part, "==") {
			kv := strings.Split(part, "==")
			if len(kv) != 2 {
				return false
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if labels[k] != v {
				return false
			}
		} else if strings.Contains(part, "=") {
			kv := strings.Split(part, "=")
			if len(kv) != 2 {
				return false
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if labels[k] != v {
				return false
			}
		} else {
			if _, exists := labels[part]; !exists {
				return false
			}
		}
	}
	return true
}

func MatchFields(pod *types.Pod, selector string) bool {
	if selector == "" {
		return true
	}
	parts := strings.Split(selector, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var isNot bool
		var kv []string
		if strings.Contains(part, "!=") {
			isNot = true
			kv = strings.Split(part, "!=")
		} else if strings.Contains(part, "==") {
			kv = strings.Split(part, "==")
		} else if strings.Contains(part, "=") {
			kv = strings.Split(part, "=")
		} else {
			return false
		}

		if len(kv) != 2 {
			return false
		}

		field := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		var actualVal string
		switch field {
		case "metadata.name":
			actualVal = pod.Metadata.Name
		case "metadata.namespace":
			actualVal = pod.Metadata.Namespace
		case "status.phase":
			actualVal = string(pod.Status.Phase)
		default:
			continue
		}

		if isNot {
			if actualVal == val {
				return false
			}
		} else {
			if actualVal != val {
				return false
			}
		}
	}
	return true
}

func MatchService(svc *types.Service, labelSelector, fieldSelector string) bool {
	if !MatchLabels(svc.Metadata.Labels, labelSelector) {
		return false
	}
	return MatchMetadataFields(svc.Metadata.Name, svc.Metadata.Namespace, fieldSelector)
}

func MatchConfigMap(cm *types.ConfigMap, labelSelector, fieldSelector string) bool {
	if !MatchLabels(cm.Metadata.Labels, labelSelector) {
		return false
	}
	return MatchMetadataFields(cm.Metadata.Name, cm.Metadata.Namespace, fieldSelector)
}

func MatchSecret(sec *types.Secret, labelSelector, fieldSelector string) bool {
	if !MatchLabels(sec.Metadata.Labels, labelSelector) {
		return false
	}
	return MatchMetadataFields(sec.Metadata.Name, sec.Metadata.Namespace, fieldSelector)
}

func MatchMetadataFields(name, namespace, selector string) bool {
	if selector == "" {
		return true
	}
	parts := strings.Split(selector, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var isNot bool
		var kv []string
		if strings.Contains(part, "!=") {
			isNot = true
			kv = strings.Split(part, "!=")
		} else if strings.Contains(part, "==") {
			kv = strings.Split(part, "==")
		} else if strings.Contains(part, "=") {
			kv = strings.Split(part, "=")
		} else {
			return false
		}
		if len(kv) != 2 {
			return false
		}
		field := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		var actualVal string
		switch field {
		case "metadata.name":
			actualVal = name
		case "metadata.namespace":
			actualVal = namespace
		default:
			continue
		}

		if isNot {
			if actualVal == val {
				return false
			}
		} else {
			if actualVal != val {
				return false
			}
		}
	}
	return true
}
