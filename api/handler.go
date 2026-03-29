package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
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

	pods := s.allPods(ns)
	if pods == nil {
		pods = []types.Pod{}
	}

	for i := range pods {
		if status, managed := s.lifecycle.Status(pods[i].Metadata.Namespace, pods[i].Metadata.Name); managed {
			pods[i].Status = status
		}
	}

	list := types.PodList{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "PodList"},
		Items:    pods,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
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

	svcs := s.services.List(ns)
	if svcs == nil {
		svcs = []types.Service{}
	}

	list := types.ServiceList{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "ServiceList"},
		Items:    svcs,
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

