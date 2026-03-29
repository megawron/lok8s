package service

import (
	"fmt"
	"strings"

	"github.com/megawron/lok8s/types"
)

func GenerateServiceEnv(namespace string, store *Store, pm *ProxyManager) []types.EnvVar {
	var envs []types.EnvVar

	services := store.List(namespace)
	for _, svc := range services {
		nameUpper := strings.ToUpper(svc.Metadata.Name)
		nameUpper = strings.ReplaceAll(nameUpper, "-", "_")

		hostKey := fmt.Sprintf("%s_SERVICE_HOST", nameUpper)
		portKey := fmt.Sprintf("%s_SERVICE_PORT", nameUpper)

		port := 0
		key := svc.Metadata.Namespace + "/" + svc.Metadata.Name
		
		pm.mu.Lock()
		if sp, ok := pm.proxies[key]; ok {
			port = sp.port
		}
		pm.mu.Unlock()

		if port == 0 && len(svc.Spec.Ports) > 0 {
			port = svc.Spec.Ports[0].Port
		}

		envs = append(envs, types.EnvVar{Name: hostKey, Value: "127.0.0.1"})
		envs = append(envs, types.EnvVar{Name: portKey, Value: fmt.Sprintf("%d", port)})
	}

	return envs
}
