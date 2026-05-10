package controller

import (
	"sync"

	"github.com/megawron/lok8s/types"
)

type Store struct {
	deployments sync.Map
	replicaSets sync.Map
}

func NewStore() *Store {
	return &Store{}
}

// Deployments CRUD
func (s *Store) StoreDeployment(dep types.Deployment) {
	key := dep.Metadata.Namespace + "/" + dep.Metadata.Name
	s.deployments.Store(key, dep)
}

func (s *Store) LoadDeployment(namespace, name string) (types.Deployment, bool) {
	key := namespace + "/" + name
	val, ok := s.deployments.Load(key)
	if !ok {
		return types.Deployment{}, false
	}
	return val.(types.Deployment), true
}

func (s *Store) DeleteDeployment(namespace, name string) {
	key := namespace + "/" + name
	s.deployments.Delete(key)
}

func (s *Store) ListDeployments(namespace string) []types.Deployment {
	var result []types.Deployment
	s.deployments.Range(func(_, value any) bool {
		dep := value.(types.Deployment)
		if namespace == "" || dep.Metadata.Namespace == namespace {
			result = append(result, dep)
		}
		return true
	})
	return result
}

// ReplicaSets CRUD
func (s *Store) StoreReplicaSet(rs types.ReplicaSet) {
	key := rs.Metadata.Namespace + "/" + rs.Metadata.Name
	s.replicaSets.Store(key, rs)
}

func (s *Store) LoadReplicaSet(namespace, name string) (types.ReplicaSet, bool) {
	key := namespace + "/" + name
	val, ok := s.replicaSets.Load(key)
	if !ok {
		return types.ReplicaSet{}, false
	}
	return val.(types.ReplicaSet), true
}

func (s *Store) DeleteReplicaSet(namespace, name string) {
	key := namespace + "/" + name
	s.replicaSets.Delete(key)
}

func (s *Store) ListReplicaSets(namespace string) []types.ReplicaSet {
	var result []types.ReplicaSet
	s.replicaSets.Range(func(_, value any) bool {
		rs := value.(types.ReplicaSet)
		if namespace == "" || rs.Metadata.Namespace == namespace {
			result = append(result, rs)
		}
		return true
	})
	return result
}
