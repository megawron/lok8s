package api

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/discovery"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/manifest"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/service"
	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
)

type Server struct {
	httpServer      *http.Server
	lifecycle       *engine.LifecycleManager
	services        *service.Store
	proxyManager    *service.ProxyManager
	configStore     *config.Store
	controllerStore *controller.Store
	pods            sync.Map
	db              *store.DB
	portPool        *network.PortPool
}

func NewServer(addr string, lifecycle *engine.LifecycleManager, portPool *network.PortPool, configStore *config.Store, controllerStore *controller.Store, db *store.DB) *Server {
	s := &Server{
		lifecycle:       lifecycle,
		services:        service.NewStore(db),
		proxyManager:    service.NewProxyManager(lifecycle, portPool),
		configStore:     configStore,
		controllerStore: controllerStore,
		db:              db,
		portPool:        portPool,
	}

	if db != nil {
		_ = db.List("pods", func(key, val []byte) error {
			var pod types.Pod
			if err := json.Unmarshal(val, &pod); err == nil {
				s.pods.Store(string(key), pod)
			}
			return nil
		})
	}


	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/namespaces/{ns}/pods", s.handleCreatePod)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods/{name}/log", s.handleGetPodLogs)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods/{name}", s.handleGetPod)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/pods", s.handleListPods)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/pods/{name}", s.handleDeletePod)

	mux.HandleFunc("POST /api/v1/namespaces/{ns}/services", s.handleCreateService)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/services", s.handleListServices)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/services/{name}", s.handleGetService)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/services/{name}", s.handleDeleteService)

	mux.HandleFunc("POST /api/v1/namespaces/{ns}/configmaps", s.handleCreateConfigMap)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/configmaps", s.handleListConfigMaps)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/configmaps/{name}", s.handleGetConfigMap)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/configmaps/{name}", s.handleDeleteConfigMap)

	mux.HandleFunc("POST /api/v1/namespaces/{ns}/secrets", s.handleCreateSecret)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/secrets", s.handleListSecrets)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/secrets/{name}", s.handleGetSecret)
	mux.HandleFunc("DELETE /api/v1/namespaces/{ns}/secrets/{name}", s.handleDeleteSecret)

	// Deployments & ReplicaSets routes
	mux.HandleFunc("POST /apis/apps/v1/namespaces/{ns}/deployments", s.handleCreateDeployment)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/deployments", s.handleListDeployments)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/deployments/{name}", s.handleGetDeployment)
	mux.HandleFunc("PUT /apis/apps/v1/namespaces/{ns}/deployments/{name}", s.handleUpdateDeployment)
	mux.HandleFunc("DELETE /apis/apps/v1/namespaces/{ns}/deployments/{name}", s.handleDeleteDeployment)

	mux.HandleFunc("POST /apis/apps/v1/namespaces/{ns}/replicasets", s.handleCreateReplicaSet)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/replicasets", s.handleListReplicaSets)
	mux.HandleFunc("GET /apis/apps/v1/namespaces/{ns}/replicasets/{name}", s.handleGetReplicaSet)
	mux.HandleFunc("PUT /apis/apps/v1/namespaces/{ns}/replicasets/{name}", s.handleUpdateReplicaSet)
	mux.HandleFunc("DELETE /apis/apps/v1/namespaces/{ns}/replicasets/{name}", s.handleDeleteReplicaSet)

	// K8s compatibility discovery routes
	mux.HandleFunc("GET /api", discovery.HandleAPIRoot)
	mux.HandleFunc("GET /api/v1", discovery.HandleAPIV1)
	mux.HandleFunc("GET /apis", discovery.HandleAPIs)
	mux.HandleFunc("GET /apis/apps/v1", discovery.HandleAPIsAppsV1)
	mux.HandleFunc("GET /version", discovery.HandleVersion)
	mux.HandleFunc("GET /.well-known/openid-configuration", discovery.HandleOpenIDConfig)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

func (s *Server) storePod(pod types.Pod) {
	key := pod.Metadata.Namespace + "/" + pod.Metadata.Name
	s.pods.Store(key, pod)
	if s.db != nil {
		_ = s.db.Put("pods", key, pod)
	}
}

func (s *Server) loadPod(namespace, name string) (types.Pod, bool) {
	val, ok := s.pods.Load(namespace + "/" + name)
	if !ok {
		return types.Pod{}, false
	}
	return val.(types.Pod), true
}

func (s *Server) deletePod(namespace, name string) {
	key := namespace + "/" + name
	s.pods.Delete(key)
	if s.db != nil {
		_ = s.db.Delete("pods", key)
	}
}

func (s *Server) allPods(namespace string) []types.Pod {
	var result []types.Pod
	s.pods.Range(func(_, value any) bool {
		pod := value.(types.Pod)
		if namespace == "" || pod.Metadata.Namespace == namespace {
			result = append(result, pod)
		}
		return true
	})
	return result
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}

	log.Printf("lok8s apiserver listening on %s", ln.Addr().String())
	return s.httpServer.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("lok8s apiserver shutting down")
	s.proxyManager.Shutdown()
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) RecoverState() {
	log.Println("Recovering lok8s state...")

	// 1. Recover Service Proxies
	svcs := s.services.List("")
	for _, svc := range svcs {
		if len(svc.Spec.Ports) > 0 {
			nodePort := svc.Spec.Ports[0].NodePort
			if nodePort > 0 {
				key := svc.Metadata.Namespace + "/" + svc.Metadata.Name
				if s.portPool != nil {
					_ = s.portPool.Reserve(key, nodePort)
				}
				_, err := s.proxyManager.StartProxy(&svc)
				if err != nil {
					log.Printf("Failed to recover service proxy for %s: %v", key, err)
				} else {
					log.Printf("Recovered service proxy for %s on port %d", key, nodePort)
				}
			}
		}
	}

	// 2. Recover Pods
	pods := s.allPods("")
	for _, pod := range pods {
		if pod.Status.Phase == types.PodRunning || pod.Status.Phase == types.PodPending {
			key := pod.Metadata.Namespace + "/" + pod.Metadata.Name
			if pod.Status.HostPort > 0 && s.portPool != nil {
				_ = s.portPool.Reserve(key, pod.Status.HostPort)
			}
			log.Printf("Recovering pod %s", key)
			err := s.LaunchPod(&pod)
			if err != nil {
				log.Printf("Failed to relaunch pod %s: %v", key, err)
			} else {
				log.Printf("Relaunched pod %s successfully", key)
			}
		}
	}
}


// PodManager implementation methods
func (s *Server) StorePod(pod types.Pod) {
	s.storePod(pod)
}

func (s *Server) DeletePod(namespace, name string) error {
	_ = s.lifecycle.Terminate(namespace, name)
	s.deletePod(namespace, name)
	return nil
}

func (s *Server) AllPods(namespace string) []types.Pod {
	return s.allPods(namespace)
}

func (s *Server) ListPods(namespace string) []types.Pod {
	pods := s.allPods(namespace)
	for i := range pods {
		if status, managed := s.lifecycle.Status(pods[i].Metadata.Namespace, pods[i].Metadata.Name); managed {
			pods[i].Status = status
		}
	}
	return pods
}

func (s *Server) LaunchPod(pod *types.Pod) error {
	s.storePod(*pod)

	engineType, target, err := manifest.ExtractEngineConfig(pod)
	if err != nil {
		pod.Status.Phase = types.PodFailed
		pod.Status.Message = err.Error()
		s.storePod(*pod)
		return err
	}

	env := manifest.CollectEnvVars(pod.Spec.Containers)
	serviceEnvs := service.GenerateServiceEnv(pod.Metadata.Namespace, s.services, s.proxyManager)
	env = append(env, serviceEnvs...)

	pod.Status = types.PodStatus{
		Phase:     types.PodPending,
		StartTime: time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.lifecycle.Launch(pod, engineType, target, env); err != nil {
		pod.Status.Phase = types.PodFailed
		pod.Status.Message = err.Error()
		s.storePod(*pod)
		return err
	}

	pod.Status.Phase = types.PodRunning
	s.storePod(*pod)
	return nil
}
