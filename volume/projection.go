package volume

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/types"
)

func ProjectVolumes(pod *types.Pod, configStore *config.Store) (map[string]string, error) {
	if pod.Metadata.UID == "" {
		return nil, fmt.Errorf("pod UID is required for volume projection")
	}

	projected := make(map[string]string)

	for _, vol := range pod.Spec.Volumes {
		volDir := filepath.Join(os.TempDir(), "lok8s-volumes", pod.Metadata.UID, vol.Name)
		if err := os.MkdirAll(volDir, 0755); err != nil {
			CleanupVolumes(pod)
			return nil, fmt.Errorf("failed to create directory for volume %q: %w", vol.Name, err)
		}

		if vol.ConfigMap != nil {
			cm, ok := configStore.LoadConfigMap(pod.Metadata.Namespace, vol.ConfigMap.Name)
			if !ok {
				CleanupVolumes(pod)
				return nil, fmt.Errorf("configmap %q not found for volume %q", vol.ConfigMap.Name, vol.Name)
			}
			for k, v := range cm.Data {
				filePath := filepath.Join(volDir, k)
				if err := os.WriteFile(filePath, []byte(v), 0644); err != nil {
					CleanupVolumes(pod)
					return nil, fmt.Errorf("failed to write configmap key %q for volume %q: %w", k, vol.Name, err)
				}
			}
		} else if vol.Secret != nil {
			sec, ok := configStore.LoadSecret(pod.Metadata.Namespace, vol.Secret.SecretName)
			if !ok {
				CleanupVolumes(pod)
				return nil, fmt.Errorf("secret %q not found for volume %q", vol.Secret.SecretName, vol.Name)
			}
			for k, v := range sec.Data {
				filePath := filepath.Join(volDir, k)
				if err := os.WriteFile(filePath, v, 0400); err != nil {
					CleanupVolumes(pod)
					return nil, fmt.Errorf("failed to write secret key %q for volume %q: %w", k, vol.Name, err)
				}
			}
		}

		projected[vol.Name] = volDir
	}

	return projected, nil
}

func CleanupVolumes(pod *types.Pod) {
	if pod.Metadata.UID == "" {
		return
	}
	podVolumesRoot := filepath.Join(os.TempDir(), "lok8s-volumes", pod.Metadata.UID)
	_ = os.RemoveAll(podVolumesRoot)
}
