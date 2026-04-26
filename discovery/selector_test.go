package discovery

import (
	"testing"

	"github.com/megawron/lok8s/types"
)

func TestMatchPod(t *testing.T) {
	pod := &types.Pod{
		Metadata: types.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app":  "web",
				"env":  "production",
				"tier": "frontend",
			},
		},
		Status: types.PodStatus{
			Phase: types.PodRunning,
		},
	}

	tests := []struct {
		name          string
		labelSelector string
		fieldSelector string
		want          bool
	}{
		{"empty selectors", "", "", true},
		{"label match", "app=web", "", true},
		{"label mismatch", "app=db", "", false},
		{"label double equals", "env==production", "", true},
		{"label not equals match", "tier!=backend", "", true},
		{"label not equals mismatch", "tier!=frontend", "", false},
		{"label exists", "env", "", true},
		{"label not exists", "nonexistent", "", false},
		{"multiple label match", "app=web,env=production", "", true},
		{"multiple label one mismatch", "app=web,env=staging", "", false},
		{"field name match", "", "metadata.name=test-pod", true},
		{"field name mismatch", "", "metadata.name=other", false},
		{"field namespace match", "", "metadata.namespace=default", true},
		{"field phase match", "", "status.phase=Running", true},
		{"field name not equal match", "", "metadata.name!=other-pod", true},
		{"field name not equal mismatch", "", "metadata.name!=test-pod", false},
		{"both match", "app=web", "status.phase=Running", true},
		{"both match but field mismatch", "app=web", "status.phase=Failed", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchPod(pod, tt.labelSelector, tt.fieldSelector)
			if got != tt.want {
				t.Errorf("MatchPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchService(t *testing.T) {
	svc := &types.Service{
		Metadata: types.ObjectMeta{
			Name:      "test-svc",
			Namespace: "kube-system",
			Labels: map[string]string{
				"k8s-app": "kube-dns",
			},
		},
	}

	if !MatchService(svc, "k8s-app=kube-dns", "metadata.name=test-svc") {
		t.Error("Expected MatchService to return true")
	}

	if MatchService(svc, "k8s-app=kube-dns", "metadata.namespace=default") {
		t.Error("Expected MatchService to return false due to namespace mismatch")
	}
}

func TestMatchConfigMap(t *testing.T) {
	cm := &types.ConfigMap{
		Metadata: types.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
			Labels: map[string]string{
				"app": "web",
			},
		},
	}

	if !MatchConfigMap(cm, "app=web", "metadata.name=test-cm") {
		t.Error("Expected MatchConfigMap to return true")
	}
}

func TestMatchSecret(t *testing.T) {
	sec := &types.Secret{
		Metadata: types.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
			Labels: map[string]string{
				"app": "db",
			},
		},
	}

	if !MatchSecret(sec, "app=db", "metadata.name=test-secret") {
		t.Error("Expected MatchSecret to return true")
	}
}
