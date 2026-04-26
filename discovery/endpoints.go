package discovery

import (
	"encoding/json"
	"net/http"

	"github.com/megawron/lok8s/types"
)

func HandleAPIRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.APIGroupList{
		Kind:        "APIVersions",
		APIVersions: []string{"v1"},
	})
}

func HandleAPIs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"kind":   "APIGroupList",
		"groups": []interface{}{},
	})
}

func HandleAPIV1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	resources := []types.APIResource{
		{Name: "pods", Namespaced: true, Kind: "Pod", Verbs: []string{"create", "delete", "get", "list", "watch"}},
		{Name: "pods/log", Namespaced: true, Kind: "Pod", Verbs: []string{"get"}},
		{Name: "services", Namespaced: true, Kind: "Service", Verbs: []string{"create", "delete", "get", "list"}},
		{Name: "configmaps", Namespaced: true, Kind: "ConfigMap", Verbs: []string{"create", "delete", "get", "list"}},
		{Name: "secrets", Namespaced: true, Kind: "Secret", Verbs: []string{"create", "delete", "get", "list"}},
	}

	json.NewEncoder(w).Encode(types.APIResourceList{
		Kind:         "APIResourceList",
		GroupVersion: "v1",
		APIResources: resources,
	})
}

func HandleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"major":        "1",
		"minor":        "29",
		"gitVersion":   "v1.29.0",
		"gitCommit":    "3f7f50a30b0431868357879e68d37449c25f4ab3",
		"gitTreeState": "clean",
		"buildDate":    "2023-12-13T08:51:44Z",
		"goVersion":    "go1.21.5",
		"compiler":     "gc",
		"platform":     "linux/amd64",
	})
}

func HandleOpenIDConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"issuer":                                "https://kubernetes.default.svc.cluster.local",
		"jwks_uri":                              "https://kubernetes.default.svc.cluster.local/openid/v1/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}
