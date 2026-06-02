package controller

import (
	"encoding/json"
	"sync"

	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
)

type Store struct {
	deployments sync.Map
	replicaSets sync.Map
	db          *store.DB
}

func NewStore(db *store.DB) *Store {
	s := &Store{db: db}
	if db != nil {
		_ = db.List("deployments", func(key, val []byte) error {
			var dep types.Deployment
			if err := json.Unmarshal(val, &dep); err == nil {
				s.deployments.Store(string(key), dep)
			}
			return nil
		})
		_ = db.List("replicasets", func(key, val []byte) error {
			var rs types.ReplicaSet
			if err := json.Unmarshal(val, &rs); err == nil {
				s.replicaSets.Store(string(key), rs)
			}
			return nil
		})
	}
	return s
}

// Deployments CRUD
func (s *Store) StoreDeployment(dep types.Deployment) {
	key := dep.Metadata.Namespace + "/" + dep.Metadata.Name
	s.deployments.Store(key, dep)
	if s.db != nil {
		_ = s.db.Put("deployments", key, dep)
	}
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
	if s.db != nil {
		_ = s.db.Delete("deployments", key)
	}
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
	if s.db != nil {
		_ = s.db.Put("replicasets", key, rs)
	}
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
	if s.db != nil {
		_ = s.db.Delete("replicasets", key)
	}
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
