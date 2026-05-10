package discovery

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/megawron/lok8s/types"
)

func ConvertPodsToTable(pods []types.Pod) *types.Table {
	t := &types.Table{
		TypeMeta: types.TypeMeta{
			APIVersion: "meta.k8s.io/v1",
			Kind:       "Table",
		},
		ColumnDefinitions: []types.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name", Description: "Name of the pod"},
			{Name: "Ready", Type: "string", Description: "Ready status"},
			{Name: "Status", Type: "string", Description: "Phase status"},
			{Name: "Restarts", Type: "string", Description: "Number of restarts"},
			{Name: "Age", Type: "string", Description: "Creation age"},
		},
		Rows: make([]types.TableRow, 0, len(pods)),
	}

	for _, pod := range pods {
		readyStr := "0/1"
		if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready {
			readyStr = "1/1"
		} else if pod.Status.Phase == types.PodSucceeded {
			readyStr = "0/1"
		}

		restarts := 0
		if len(pod.Status.ContainerStatuses) > 0 {
			restarts = pod.Status.ContainerStatuses[0].RestartCount
		} else {
			restarts = pod.Status.RestartCount
		}

		ageStr := translateTimestamp(pod.Metadata.CreationTimestamp)
		podBytes, _ := json.Marshal(pod)

		row := types.TableRow{
			Cells: []interface{}{
				pod.Metadata.Name,
				readyStr,
				string(pod.Status.Phase),
				fmt.Sprintf("%d", restarts),
				ageStr,
			},
			Object: types.RawExtension{
				Raw: podBytes,
			},
		}
		t.Rows = append(t.Rows, row)
	}

	return t
}

func ConvertConfigMapsToTable(cms []types.ConfigMap) *types.Table {
	t := &types.Table{
		TypeMeta: types.TypeMeta{
			APIVersion: "meta.k8s.io/v1",
			Kind:       "Table",
		},
		ColumnDefinitions: []types.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name"},
			{Name: "Data", Type: "integer"},
			{Name: "Age", Type: "string"},
		},
		Rows: make([]types.TableRow, 0, len(cms)),
	}
	for _, cm := range cms {
		cmBytes, _ := json.Marshal(cm)
		row := types.TableRow{
			Cells: []interface{}{
				cm.Metadata.Name,
				len(cm.Data),
				translateTimestamp(cm.Metadata.CreationTimestamp),
			},
			Object: types.RawExtension{Raw: cmBytes},
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func ConvertSecretsToTable(secs []types.Secret) *types.Table {
	t := &types.Table{
		TypeMeta: types.TypeMeta{
			APIVersion: "meta.k8s.io/v1",
			Kind:       "Table",
		},
		ColumnDefinitions: []types.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name"},
			{Name: "Type", Type: "string"},
			{Name: "Data", Type: "integer"},
			{Name: "Age", Type: "string"},
		},
		Rows: make([]types.TableRow, 0, len(secs)),
	}
	for _, sec := range secs {
		secBytes, _ := json.Marshal(sec)
		row := types.TableRow{
			Cells: []interface{}{
				sec.Metadata.Name,
				sec.Type,
				len(sec.Data),
				translateTimestamp(sec.Metadata.CreationTimestamp),
			},
			Object: types.RawExtension{Raw: secBytes},
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func ConvertServicesToTable(svcs []types.Service) *types.Table {
	t := &types.Table{
		TypeMeta: types.TypeMeta{
			APIVersion: "meta.k8s.io/v1",
			Kind:       "Table",
		},
		ColumnDefinitions: []types.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name"},
			{Name: "Type", Type: "string"},
			{Name: "Cluster-IP", Type: "string"},
			{Name: "External-IP", Type: "string"},
			{Name: "Port(s)", Type: "string"},
			{Name: "Age", Type: "string"},
		},
		Rows: make([]types.TableRow, 0, len(svcs)),
	}
	for _, svc := range svcs {
		portsStr := "<none>"
		if len(svc.Spec.Ports) > 0 {
			p := svc.Spec.Ports[0]
			proto := p.Protocol
			if proto == "" {
				proto = "TCP"
			}
			portsStr = fmt.Sprintf("%d/%s", p.Port, strings.ToUpper(proto))
			if p.NodePort > 0 {
				portsStr += fmt.Sprintf(":%d", p.NodePort)
			}
		}

		svcBytes, _ := json.Marshal(svc)
		row := types.TableRow{
			Cells: []interface{}{
				svc.Metadata.Name,
				svc.Spec.Type,
				"127.0.0.1",
				"<none>",
				portsStr,
				translateTimestamp(svc.Metadata.CreationTimestamp),
			},
			Object: types.RawExtension{Raw: svcBytes},
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func translateTimestamp(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}
	diff := time.Since(timestamp)
	if diff < time.Second {
		return "0s"
	}
	if diff < time.Minute {
		return fmt.Sprintf("%ds", int(diff.Seconds()))
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%dh", int(diff.Hours()))
	}
	return fmt.Sprintf("%dd", int(diff.Hours()/24))
}

func ConvertDeploymentsToTable(deps []types.Deployment) *types.Table {
	t := &types.Table{
		TypeMeta: types.TypeMeta{
			APIVersion: "meta.k8s.io/v1",
			Kind:       "Table",
		},
		ColumnDefinitions: []types.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name"},
			{Name: "Ready", Type: "string"},
			{Name: "Up-to-date", Type: "integer"},
			{Name: "Available", Type: "integer"},
			{Name: "Age", Type: "string"},
		},
		Rows: make([]types.TableRow, 0, len(deps)),
	}

	for _, dep := range deps {
		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		readyStr := fmt.Sprintf("%d/%d", dep.Status.ReadyReplicas, desired)
		depBytes, _ := json.Marshal(dep)
		row := types.TableRow{
			Cells: []interface{}{
				dep.Metadata.Name,
				readyStr,
				int(dep.Status.UpdatedReplicas),
				int(dep.Status.AvailableReplicas),
				translateTimestamp(dep.Metadata.CreationTimestamp),
			},
			Object: types.RawExtension{Raw: depBytes},
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func ConvertReplicaSetsToTable(rss []types.ReplicaSet) *types.Table {
	t := &types.Table{
		TypeMeta: types.TypeMeta{
			APIVersion: "meta.k8s.io/v1",
			Kind:       "Table",
		},
		ColumnDefinitions: []types.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name"},
			{Name: "Desired", Type: "integer"},
			{Name: "Current", Type: "integer"},
			{Name: "Ready", Type: "integer"},
			{Name: "Age", Type: "string"},
		},
		Rows: make([]types.TableRow, 0, len(rss)),
	}

	for _, rs := range rss {
		desired := int32(1)
		if rs.Spec.Replicas != nil {
			desired = *rs.Spec.Replicas
		}
		rsBytes, _ := json.Marshal(rs)
		row := types.TableRow{
			Cells: []interface{}{
				rs.Metadata.Name,
				int(desired),
				int(rs.Status.Replicas),
				int(rs.Status.ReadyReplicas),
				translateTimestamp(rs.Metadata.CreationTimestamp),
			},
			Object: types.RawExtension{Raw: rsBytes},
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}
