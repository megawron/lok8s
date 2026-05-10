package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/megawron/lok8s/discovery"
	"github.com/megawron/lok8s/types"
)

// Deployments Handlers
func (s *Server) handleCreateDeployment(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var dep types.Deployment
	if err := json.Unmarshal(body, &dep); err != nil {
		writeStatus(w, http.StatusBadRequest, fmt.Sprintf("invalid deployment json: %v", err))
		return
	}

	if dep.Metadata.Namespace == "" {
		dep.Metadata.Namespace = ns
	}
	dep.APIVersion = "apps/v1"
	dep.Kind = "Deployment"
	dep.Metadata.UID = uuid.New().String()
	dep.Metadata.CreationTimestamp = time.Now().UTC()

	s.controllerStore.StoreDeployment(dep)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dep)
}

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	q := r.URL.Query()
	labelSelector := q.Get("labelSelector")
	fieldSelector := q.Get("fieldSelector")

	deps := s.controllerStore.ListDeployments(ns)
	if deps == nil {
		deps = []types.Deployment{}
	}

	filteredDeps := make([]types.Deployment, 0, len(deps))
	for i := range deps {
		if discovery.MatchMetadataFields(deps[i].Metadata.Name, deps[i].Metadata.Namespace, fieldSelector) &&
			discovery.MatchLabels(deps[i].Metadata.Labels, labelSelector) {
			filteredDeps = append(filteredDeps, deps[i])
		}
	}

	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "as=Table") {
		table := discovery.ConvertDeploymentsToTable(filteredDeps)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)
		return
	}

	list := types.DeploymentList{
		TypeMeta: types.TypeMeta{APIVersion: "apps/v1", Kind: "DeploymentList"},
		Items:    filteredDeps,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	dep, ok := s.controllerStore.LoadDeployment(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "deployment not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dep)
}

func (s *Server) handleUpdateDeployment(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	var dep types.Deployment
	if err := json.Unmarshal(body, &dep); err != nil {
		writeStatus(w, http.StatusBadRequest, fmt.Sprintf("invalid deployment json: %v", err))
		return
	}

	existing, ok := s.controllerStore.LoadDeployment(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "deployment not found")
		return
	}

	existing.Spec = dep.Spec
	existing.Metadata.Labels = dep.Metadata.Labels
	existing.Metadata.Annotations = dep.Metadata.Annotations
	existing.Metadata.ResourceVersion = uuid.New().String()

	s.controllerStore.StoreDeployment(existing)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
}

func (s *Server) handleDeleteDeployment(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	_, ok := s.controllerStore.LoadDeployment(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "deployment not found")
		return
	}

	// Cascading delete
	rss := s.controllerStore.ListReplicaSets(ns)
	for _, rs := range rss {
		if rs.Metadata.Annotations != nil && rs.Metadata.Annotations["lok8s.io/owner"] == "Deployment/"+name {
			s.cascadeDeleteReplicaSet(rs)
		}
	}

	s.controllerStore.DeleteDeployment(ns, name)
	log.Printf("deployment %s/%s deleted", ns, name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.StatusResponse{
		Kind:    "Status",
		Status:  "Success",
		Message: "deployment deleted",
		Code:    http.StatusOK,
	})
}

// ReplicaSets Handlers
func (s *Server) handleCreateReplicaSet(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var rs types.ReplicaSet
	if err := json.Unmarshal(body, &rs); err != nil {
		writeStatus(w, http.StatusBadRequest, fmt.Sprintf("invalid replicaset json: %v", err))
		return
	}

	if rs.Metadata.Namespace == "" {
		rs.Metadata.Namespace = ns
	}
	rs.APIVersion = "apps/v1"
	rs.Kind = "ReplicaSet"
	rs.Metadata.UID = uuid.New().String()
	rs.Metadata.CreationTimestamp = time.Now().UTC()

	s.controllerStore.StoreReplicaSet(rs)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rs)
}

func (s *Server) handleListReplicaSets(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	q := r.URL.Query()
	labelSelector := q.Get("labelSelector")
	fieldSelector := q.Get("fieldSelector")

	rss := s.controllerStore.ListReplicaSets(ns)
	if rss == nil {
		rss = []types.ReplicaSet{}
	}

	filteredRss := make([]types.ReplicaSet, 0, len(rss))
	for i := range rss {
		if discovery.MatchMetadataFields(rss[i].Metadata.Name, rss[i].Metadata.Namespace, fieldSelector) &&
			discovery.MatchLabels(rss[i].Metadata.Labels, labelSelector) {
			filteredRss = append(filteredRss, rss[i])
		}
	}

	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "as=Table") {
		table := discovery.ConvertReplicaSetsToTable(filteredRss)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)
		return
	}

	list := types.ReplicaSetList{
		TypeMeta: types.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSetList"},
		Items:    filteredRss,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleGetReplicaSet(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	rs, ok := s.controllerStore.LoadReplicaSet(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "replicaset not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rs)
}

func (s *Server) handleUpdateReplicaSet(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	var rs types.ReplicaSet
	if err := json.Unmarshal(body, &rs); err != nil {
		writeStatus(w, http.StatusBadRequest, fmt.Sprintf("invalid replicaset json: %v", err))
		return
	}

	existing, ok := s.controllerStore.LoadReplicaSet(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "replicaset not found")
		return
	}

	existing.Spec = rs.Spec
	existing.Metadata.Labels = rs.Metadata.Labels
	existing.Metadata.Annotations = rs.Metadata.Annotations
	existing.Metadata.ResourceVersion = uuid.New().String()

	s.controllerStore.StoreReplicaSet(existing)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
}

func (s *Server) handleDeleteReplicaSet(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	rs, ok := s.controllerStore.LoadReplicaSet(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "replicaset not found")
		return
	}

	s.cascadeDeleteReplicaSet(rs)
	log.Printf("replicaset %s/%s deleted", ns, name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.StatusResponse{
		Kind:    "Status",
		Status:  "Success",
		Message: "replicaset deleted",
		Code:    http.StatusOK,
	})
}

func (s *Server) cascadeDeleteReplicaSet(rs types.ReplicaSet) {
	pods := s.AllPods(rs.Metadata.Namespace)
	for _, pod := range pods {
		if pod.Metadata.Annotations != nil && pod.Metadata.Annotations["lok8s.io/owner"] == "ReplicaSet/"+rs.Metadata.Name {
			_ = s.lifecycle.Terminate(pod.Metadata.Namespace, pod.Metadata.Name)
			s.DeletePod(pod.Metadata.Namespace, pod.Metadata.Name)
		}
	}
	s.controllerStore.DeleteReplicaSet(rs.Metadata.Namespace, rs.Metadata.Name)
}
