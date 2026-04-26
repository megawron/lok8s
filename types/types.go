package types

import "time"

type TypeMeta struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
}

type ObjectMeta struct {
	Name              string            `json:"name" yaml:"name"`
	Namespace         string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty" yaml:"uid,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Labels            map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	CreationTimestamp time.Time         `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}

type EnvVar struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type HTTPGetAction struct {
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
	Port   int    `json:"port" yaml:"port"`
	Scheme string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
}

type TCPSocketAction struct {
	Port int `json:"port" yaml:"port"`
}

type ExecAction struct {
	Command []string `json:"command" yaml:"command"`
}

type Probe struct {
	HTTPGet             *HTTPGetAction   `json:"httpGet,omitempty" yaml:"httpGet,omitempty"`
	TCPSocket           *TCPSocketAction `json:"tcpSocket,omitempty" yaml:"tcpSocket,omitempty"`
	Exec                *ExecAction      `json:"exec,omitempty" yaml:"exec,omitempty"`
	InitialDelaySeconds int              `json:"initialDelaySeconds,omitempty" yaml:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int              `json:"periodSeconds,omitempty" yaml:"periodSeconds,omitempty"`
	TimeoutSeconds      int              `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
	FailureThreshold    int              `json:"failureThreshold,omitempty" yaml:"failureThreshold,omitempty"`
	SuccessThreshold    int              `json:"successThreshold,omitempty" yaml:"successThreshold,omitempty"`
}

type ContainerPort struct {
	Name          string `json:"name,omitempty" yaml:"name,omitempty"`
	ContainerPort int    `json:"containerPort" yaml:"containerPort"`
	Protocol      string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

type VolumeMount struct {
	Name      string `json:"name" yaml:"name"`
	MountPath string `json:"mountPath" yaml:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
}

type Container struct {
	Name           string          `json:"name" yaml:"name"`
	Image          string          `json:"image,omitempty" yaml:"image,omitempty"`
	Command        []string        `json:"command,omitempty" yaml:"command,omitempty"`
	Args           []string        `json:"args,omitempty" yaml:"args,omitempty"`
	Env            []EnvVar        `json:"env,omitempty" yaml:"env,omitempty"`
	Ports          []ContainerPort `json:"ports,omitempty" yaml:"ports,omitempty"`
	VolumeMounts   []VolumeMount   `json:"volumeMounts,omitempty" yaml:"volumeMounts,omitempty"`
	LivenessProbe  *Probe          `json:"livenessProbe,omitempty" yaml:"livenessProbe,omitempty"`
	ReadinessProbe *Probe          `json:"readinessProbe,omitempty" yaml:"readinessProbe,omitempty"`
}

type RestartPolicy string

const (
	RestartAlways    RestartPolicy = "Always"
	RestartOnFailure RestartPolicy = "OnFailure"
	RestartNever     RestartPolicy = "Never"
)

type ConfigMapVolumeSource struct {
	Name string `json:"name" yaml:"name"`
}

type SecretVolumeSource struct {
	SecretName string `json:"secretName" yaml:"secretName"`
}

type Volume struct {
	Name      string                 `json:"name" yaml:"name"`
	ConfigMap *ConfigMapVolumeSource `json:"configMap,omitempty" yaml:"configMap,omitempty"`
	Secret    *SecretVolumeSource    `json:"secret,omitempty" yaml:"secret,omitempty"`
}

type PodSpec struct {
	Containers     []Container   `json:"containers" yaml:"containers"`
	InitContainers []Container   `json:"initContainers,omitempty" yaml:"initContainers,omitempty"`
	RestartPolicy  RestartPolicy `json:"restartPolicy,omitempty" yaml:"restartPolicy,omitempty"`
	Volumes        []Volume      `json:"volumes,omitempty" yaml:"volumes,omitempty"`
}

type PodPhase string

const (
	PodPending   PodPhase = "Pending"
	PodRunning   PodPhase = "Running"
	PodSucceeded PodPhase = "Succeeded"
	PodFailed    PodPhase = "Failed"
)

type ContainerStatus struct {
	Name         string   `json:"name"`
	Ready        bool     `json:"ready"`
	RestartCount int      `json:"restartCount"`
	State        string   `json:"state"`
	ExitCode     *int     `json:"exitCode,omitempty"`
}

type PodCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type PodStatus struct {
	Phase              PodPhase          `json:"phase"`
	Message            string            `json:"message,omitempty"`
	StartTime          string            `json:"startTime,omitempty"`
	RestartCount       int               `json:"restartCount,omitempty"`
	ContainerStatuses  []ContainerStatus `json:"containerStatuses,omitempty"`
	Conditions         []PodCondition    `json:"conditions,omitempty"`
	PodIP              string            `json:"podIP,omitempty" yaml:"podIP,omitempty"`
	HostPort           int               `json:"hostPort,omitempty" yaml:"hostPort,omitempty"`
}

type Pod struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     PodSpec    `json:"spec" yaml:"spec"`
	Status   PodStatus  `json:"status,omitempty" yaml:"status,omitempty"`
}

type PodList struct {
	TypeMeta `json:",inline"`
	Items    []Pod `json:"items"`
}

type ServicePort struct {
	Name       string `json:"name,omitempty" yaml:"name,omitempty"`
	Protocol   string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Port       int    `json:"port" yaml:"port"`
	TargetPort int    `json:"targetPort,omitempty" yaml:"targetPort,omitempty"`
	NodePort   int    `json:"nodePort,omitempty" yaml:"nodePort,omitempty"`
}

type ServiceSpec struct {
	Ports    []ServicePort     `json:"ports,omitempty" yaml:"ports,omitempty"`
	Selector map[string]string `json:"selector,omitempty" yaml:"selector,omitempty"`
	Type     string            `json:"type,omitempty" yaml:"type,omitempty"`
}

type Service struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta  `json:"metadata" yaml:"metadata"`
	Spec     ServiceSpec `json:"spec" yaml:"spec"`
	Status   interface{} `json:"status,omitempty" yaml:"status,omitempty"`
}

type ServiceList struct {
	TypeMeta `json:",inline"`
	Items    []Service `json:"items"`
}

type ConfigMap struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta        `json:"metadata" yaml:"metadata"`
	Data     map[string]string `json:"data,omitempty" yaml:"data,omitempty"`
}

type ConfigMapList struct {
	TypeMeta `json:",inline"`
	Items    []ConfigMap `json:"items"`
}

type Secret struct {
	TypeMeta   `json:",inline" yaml:",inline"`
	Metadata   ObjectMeta        `json:"metadata" yaml:"metadata"`
	Data       map[string][]byte `json:"data,omitempty" yaml:"data,omitempty"`
	StringData map[string]string `json:"stringData,omitempty" yaml:"stringData,omitempty"`
	Type       string            `json:"type,omitempty" yaml:"type,omitempty"`
}

type SecretList struct {
	TypeMeta `json:",inline"`
	Items    []Secret `json:"items"`
}

type RawExtension struct {
	Raw []byte `json:"raw,omitempty"`
}

type TableColumnDefinition struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Format      string `json:"format"`
	Description string `json:"description"`
	Priority    int32  `json:"priority"`
}

type TableRow struct {
	Cells  []interface{} `json:"cells"`
	Object RawExtension  `json:"object,omitempty"`
}

type Table struct {
	TypeMeta          `json:",inline"`
	ColumnDefinitions []TableColumnDefinition `json:"columnDefinitions"`
	Rows              []TableRow              `json:"rows"`
}

type APIGroupList struct {
	Kind        string   `json:"kind"`
	APIVersions []string `json:"versions"`
}

type APIResource struct {
	Name       string   `json:"name"`
	Namespaced bool     `json:"namespaced"`
	Kind       string   `json:"kind"`
	Verbs      []string `json:"verbs"`
}

type APIResourceList struct {
	Kind         string        `json:"kind"`
	GroupVersion string        `json:"groupVersion"`
	APIResources []APIResource `json:"resources"`
}

type WatchEvent struct {
	Type   string `json:"type"` // ADDED, MODIFIED, DELETED, ERROR
	Object Pod    `json:"object"`
}

type StatusResponse struct {
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
	Code    int    `json:"code"`
}
