package volume

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/types"
)

func TestProjectVolumes(t *testing.T) {
	store := config.NewStore(nil)

	// 1. Create a ConfigMap and a Secret in the store
	cm := types.ConfigMap{
		Metadata: types.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
		},
		Data: map[string]string{
			"config.json": `{"port": 80}`,
			"debug":       "true",
		},
	}
	store.StoreConfigMap(cm)

	sec := types.Secret{
		Metadata: types.ObjectMeta{
			Name:      "test-sec",
			Namespace: "default",
		},
		StringData: map[string]string{
			"password": "secretpassword",
		},
	}
	store.StoreSecret(sec)

	// 2. Set up Pod with volumes referencing them
	pod := &types.Pod{
		Metadata: types.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "my-test-pod-uid",
		},
		Spec: types.PodSpec{
			Volumes: []types.Volume{
				{
					Name: "vol-cm",
					ConfigMap: &types.ConfigMapVolumeSource{
						Name: "test-cm",
					},
				},
				{
					Name: "vol-sec",
					Secret: &types.SecretVolumeSource{
						SecretName: "test-sec",
					},
				},
			},
		},
	}

	// 3. Project volumes
	dirs, err := ProjectVolumes(pod, store)
	if err != nil {
		t.Fatalf("ProjectVolumes failed: %v", err)
	}

	// Check directories exist and contain files
	volCmDir, ok := dirs["vol-cm"]
	if !ok {
		t.Fatal("vol-cm directory path not returned")
	}

	data1, err := os.ReadFile(filepath.Join(volCmDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data1) != `{"port": 80}` {
		t.Errorf("unexpected config.json content: %s", string(data1))
	}

	data2, err := os.ReadFile(filepath.Join(volCmDir, "debug"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data2) != "true" {
		t.Errorf("unexpected debug content: %s", string(data2))
	}

	volSecDir, ok := dirs["vol-sec"]
	if !ok {
		t.Fatal("vol-sec directory path not returned")
	}

	secData, err := os.ReadFile(filepath.Join(volSecDir, "password"))
	if err != nil {
		t.Fatal(err)
	}
	if string(secData) != "secretpassword" {
		t.Errorf("unexpected password content: %s", string(secData))
	}

	// 4. Cleanup and verify removal
	CleanupVolumes(pod)

	if _, err := os.Stat(volCmDir); !os.IsNotExist(err) {
		t.Error("expected vol-cm directory to be deleted")
	}
	if _, err := os.Stat(volSecDir); !os.IsNotExist(err) {
		t.Error("expected vol-sec directory to be deleted")
	}
}
