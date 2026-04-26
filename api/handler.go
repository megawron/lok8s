package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/megawron/lok8s/discovery"
	"github.com/megawron/lok8s/manifest"
	"github.com/megawron/lok8s/service"
	"github.com/megawron/lok8s/types"
)

func (s *Server) handleCreatePod(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	pod, err := manifest.Parse(body)
	if err != nil {
		writeStatus(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if pod.Metadata.Namespace == "" {
		pod.Metadata.Namespace = ns
	}
	pod.Metadata.UID = uuid.New().String()
	pod.Metadata.CreationTimestamp = time.Now().UTC()
	pod.APIVersion = "v1"
	pod.Kind = "Pod"

	engineType, target, err := manifest.ExtractEngineConfig(pod)
	if err != nil {
		writeStatus(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	env := manifest.CollectEnvVars(pod.Spec.Containers)
	serviceEnvs := service.GenerateServiceEnv(ns, s.services, s.proxyManager)
	env = append(env, serviceEnvs...)

	pod.Status = types.PodStatus{
		Phase:     types.PodPending,
		StartTime: time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.lifecycle.Launch(pod, engineType, target, env); err != nil {
		pod.Status.Phase = types.PodFailed
		pod.Status.Message = err.Error()
		s.storePod(*pod)
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}

	pod.Status.Phase = types.PodRunning
	s.storePod(*pod)

	log.Printf("pod %s/%s created (engine=%s, target=%s)", ns, pod.Metadata.Name, engineType, target)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pod)
}

func (s *Server) handleGetPod(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	pod, ok := s.loadPod(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "pod not found")
		return
	}

	if status, managed := s.lifecycle.Status(ns, name); managed {
		pod.Status = status
		s.storePod(pod)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pod)
}

func (s *Server) handleListPods(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	q := r.URL.Query()
	labelSelector := q.Get("labelSelector")
	fieldSelector := q.Get("fieldSelector")
	watch := q.Get("watch") == "true" || q.Get("watch") == "1"

	if watch {
		s.handleWatchPods(w, r, ns, labelSelector, fieldSelector)
		return
	}

	pods := s.allPods(ns)
	if pods == nil {
		pods = []types.Pod{}
	}

	filteredPods := make([]types.Pod, 0, len(pods))
	for i := range pods {
		if status, managed := s.lifecycle.Status(pods[i].Metadata.Namespace, pods[i].Metadata.Name); managed {
			pods[i].Status = status
		}
		if discovery.MatchPod(&pods[i], labelSelector, fieldSelector) {
			filteredPods = append(filteredPods, pods[i])
		}
	}

	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "as=Table") {
		table := discovery.ConvertPodsToTable(filteredPods)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)
		return
	}

	list := types.PodList{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "PodList"},
		Items:    filteredPods,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleWatchPods(w http.ResponseWriter, r *http.Request, ns, labelSelector, fieldSelector string) {
	ctx := r.Context()
	ch := s.lifecycle.Watch(ctx)

	w.Header().Set("Content-Type", "application/json;stream=watch")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeStatus(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-ch:
			if !open {
				return
			}
			if ns != "" && event.Object.Metadata.Namespace != ns {
				continue
			}
			if !discovery.MatchPod(&event.Object, labelSelector, fieldSelector) {
				continue
			}

			data, err := json.Marshal(event)
			if err != nil {
				return
			}
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) handleDeletePod(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	_, ok := s.loadPod(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "pod not found")
		return
	}

	s.lifecycle.Terminate(ns, name)
	s.deletePod(ns, name)
	log.Printf("pod %s/%s deleted", ns, name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.StatusResponse{
		Kind:    "Status",
		Status:  "Success",
		Message: "pod deleted",
		Code:    http.StatusOK,
	})
}

func writeStatus(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(types.StatusResponse{
		Kind:    "Status",
		Status:  "Failure",
		Message: msg,
		Code:    code,
	})
}

func (s *Server) handleGetPodLogs(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	// Verify pod exists
	_, ok := s.loadPod(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "pod not found")
		return
	}

	rb, ok := s.lifecycle.Logs(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "pod logs not found or not running")
		return
	}

	// Read query parameters
	q := r.URL.Query()
	follow := q.Get("follow") == "true"
	tailLinesStr := q.Get("tailLines")

	var tailLines int
	if tailLinesStr != "" {
		var err error
		tailLines, err = strconv.Atoi(tailLinesStr)
		if err != nil || tailLines < 0 {
			writeStatus(w, http.StatusBadRequest, "invalid tailLines parameter")
			return
		}
	}

	// Set headers
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	
	var flusher http.Flusher
	if follow {
		var flusherOk bool
		flusher, flusherOk = w.(http.Flusher)
		if !flusherOk {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Cache-Control", "no-cache")
	}

	// Write existing / tail lines
	if tailLines > 0 {
		w.Write(rb.TailLines(tailLines))
	} else {
		w.Write(rb.ReadAll())
	}

	if follow {
		flusher.Flush()

		ctx := r.Context()
		ch := rb.Follow(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case chunk, open := <-ch:
				if !open {
					return
				}
				_, err := w.Write(chunk)
				if err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	svc, err := manifest.ParseService(body)
	if err != nil {
		writeStatus(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if svc.Metadata.Namespace == "" {
		svc.Metadata.Namespace = ns
	}
	svc.APIVersion = "v1"
	svc.Kind = "Service"
	svc.Metadata.UID = uuid.New().String()
	svc.Metadata.CreationTimestamp = time.Now().UTC()

	port, err := s.proxyManager.StartProxy(svc)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to start service proxy: %v", err))
		return
	}

	if len(svc.Spec.Ports) > 0 {
		svc.Spec.Ports[0].NodePort = port
		if svc.Spec.Ports[0].Port == 0 {
			svc.Spec.Ports[0].Port = port
		}
	}

	s.services.Store(*svc)
	log.Printf("service %s/%s created (port=%d)", ns, svc.Metadata.Name, port)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(svc)
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	q := r.URL.Query()
	labelSelector := q.Get("labelSelector")
	fieldSelector := q.Get("fieldSelector")

	svcs := s.services.List(ns)
	if svcs == nil {
		svcs = []types.Service{}
	}

	filteredSvcs := make([]types.Service, 0, len(svcs))
	for i := range svcs {
		if discovery.MatchService(&svcs[i], labelSelector, fieldSelector) {
			filteredSvcs = append(filteredSvcs, svcs[i])
		}
	}

	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "as=Table") {
		table := discovery.ConvertServicesToTable(filteredSvcs)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)
		return
	}

	list := types.ServiceList{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "ServiceList"},
		Items:    filteredSvcs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	svc, ok := s.services.Load(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "service not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(svc)
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	_, ok := s.services.Load(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "service not found")
		return
	}

	s.proxyManager.StopProxy(ns, name)
	s.services.Delete(ns, name)
	log.Printf("service %s/%s deleted", ns, name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.StatusResponse{
		Kind:    "Status",
		Status:  "Success",
		Message: "service deleted",
		Code:    http.StatusOK,
	})
}

func (s *Server) handleCreateConfigMap(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	cm, err := manifest.ParseConfigMap(body)
	if err != nil {
		writeStatus(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if cm.Metadata.Namespace == "" {
		cm.Metadata.Namespace = ns
	}
	cm.APIVersion = "v1"
	cm.Kind = "ConfigMap"
	cm.Metadata.UID = uuid.New().String()
	cm.Metadata.CreationTimestamp = time.Now().UTC()

	s.configStore.StoreConfigMap(*cm)
	log.Printf("configmap %s/%s created", ns, cm.Metadata.Name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(cm)
}

func (s *Server) handleListConfigMaps(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	q := r.URL.Query()
	labelSelector := q.Get("labelSelector")
	fieldSelector := q.Get("fieldSelector")

	cms := s.configStore.ListConfigMaps(ns)
	if cms == nil {
		cms = []types.ConfigMap{}
	}

	filteredCms := make([]types.ConfigMap, 0, len(cms))
	for i := range cms {
		if discovery.MatchConfigMap(&cms[i], labelSelector, fieldSelector) {
			filteredCms = append(filteredCms, cms[i])
		}
	}

	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "as=Table") {
		table := discovery.ConvertConfigMapsToTable(filteredCms)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)
		return
	}

	list := types.ConfigMapList{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "ConfigMapList"},
		Items:    filteredCms,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleGetConfigMap(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	cm, ok := s.configStore.LoadConfigMap(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "configmap not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cm)
}

func (s *Server) handleDeleteConfigMap(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	_, ok := s.configStore.LoadConfigMap(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "configmap not found")
		return
	}

	s.configStore.DeleteConfigMap(ns, name)
	log.Printf("configmap %s/%s deleted", ns, name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.StatusResponse{
		Kind:    "Status",
		Status:  "Success",
		Message: "configmap deleted",
		Code:    http.StatusOK,
	})
}

func (s *Server) handleCreateSecret(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	sec, err := manifest.ParseSecret(body)
	if err != nil {
		writeStatus(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if sec.Metadata.Namespace == "" {
		sec.Metadata.Namespace = ns
	}
	sec.APIVersion = "v1"
	sec.Kind = "Secret"
	sec.Metadata.UID = uuid.New().String()
	sec.Metadata.CreationTimestamp = time.Now().UTC()

	s.configStore.StoreSecret(*sec)
	log.Printf("secret %s/%s created", ns, sec.Metadata.Name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sec)
}

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	q := r.URL.Query()
	labelSelector := q.Get("labelSelector")
	fieldSelector := q.Get("fieldSelector")

	secs := s.configStore.ListSecrets(ns)
	if secs == nil {
		secs = []types.Secret{}
	}

	filteredSecs := make([]types.Secret, 0, len(secs))
	for i := range secs {
		if discovery.MatchSecret(&secs[i], labelSelector, fieldSelector) {
			filteredSecs = append(filteredSecs, secs[i])
		}
	}

	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "as=Table") {
		table := discovery.ConvertSecretsToTable(filteredSecs)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)
		return
	}

	list := types.SecretList{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "SecretList"},
		Items:    filteredSecs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleGetSecret(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	sec, ok := s.configStore.LoadSecret(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "secret not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sec)
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	_, ok := s.configStore.LoadSecret(ns, name)
	if !ok {
		writeStatus(w, http.StatusNotFound, "secret not found")
		return
	}

	s.configStore.DeleteSecret(ns, name)
	log.Printf("secret %s/%s deleted", ns, name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.StatusResponse{
		Kind:    "Status",
		Status:  "Success",
		Message: "secret deleted",
		Code:    http.StatusOK,
	})
}

