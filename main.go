package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/megawron/lok8s/api"
	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
	"gopkg.in/yaml.v3"
)

const banner = `
  ██╗      ██████╗ ██╗  ██╗ █████╗ ███████╗
  ██║     ██╔═══██╗██║ ██╔╝██╔══██╗██╔════╝
  ██║     ██║   ██║█████╔╝ ╚█████╔╝███████╗
  ██║     ██║   ██║██╔═██╗ ██╔══██╗╚════██║
  ███████╗╚██████╔╝██║  ██╗╚█████╔╝███████║
  ╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚════╝ ╚══════╝
  lightweight pod supervisor — no containers required
`

type rawMetadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type rawManifest struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   rawMetadata `yaml:"metadata"`
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "server" || os.Args[1] == "daemon" || os.Args[1] == "apiserver" || os.Args[1] == "start") {
		runServer(os.Args[2:])
		return
	}
	runCli(os.Args[1:])
}

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	_ = fs.Parse(args)

	fmt.Fprint(os.Stderr, banner)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Open local database
	db, err := store.Open("lok8s.db")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() {
		log.Println("closing database...")
		if err := db.Close(); err != nil {
			log.Printf("database close error: %v", err)
		}
	}()

	registry := engine.NewRegistry()
	registry.Register("native", engine.NewNativeEngine())
	registry.Register("wasm", engine.NewWasmEngine())

	portPool := network.NewPortPool(30000, 32767)
	configStore := config.NewStore(db)
	controllerStore := controller.NewStore(db)

	lifecycle := engine.NewLifecycleManager(registry, portPool, configStore)
	srv := api.NewServer(*addr, lifecycle, portPool, configStore, controllerStore, db)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize and start controllers
	rsController := controller.NewReplicaSetController(controllerStore, srv)
	depController := controller.NewDeploymentController(controllerStore)

	log.Println("Starting ReplicaSet and Deployment controllers")
	rsController.Start(ctx)
	depController.Start(ctx)

	// Recover state of services and pods
	srv.RecoverState()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case <-ctx.Done():
		log.Println("received shutdown signal, shutting down services and pods...")
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		lifecycle.Shutdown()
	}
}

func runCli(args []string) {
	var serverAddr string
	var namespace string

	var cleanArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-s" || arg == "--server" {
			if i+1 < len(args) {
				serverAddr = args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "-s=") {
			serverAddr = strings.TrimPrefix(arg, "-s=")
		} else if strings.HasPrefix(arg, "--server=") {
			serverAddr = strings.TrimPrefix(arg, "--server=")
		} else if arg == "-n" || arg == "--namespace" {
			if i+1 < len(args) {
				namespace = args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "-n=") {
			namespace = strings.TrimPrefix(arg, "-n=")
		} else if strings.HasPrefix(arg, "--namespace=") {
			namespace = strings.TrimPrefix(arg, "--namespace=")
		} else {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	if serverAddr == "" {
		serverAddr = "http://localhost:8080"
	}
	if namespace == "" {
		namespace = "default"
	}

	// ensure serverAddr doesn't end with slash
	serverAddr = strings.TrimSuffix(serverAddr, "/")

	if len(cleanArgs) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := cleanArgs[0]
	switch cmd {
	case "apply":
		handleApply(serverAddr, namespace, cleanArgs[1:])
	case "get":
		handleGet(serverAddr, namespace, cleanArgs[1:])
	case "delete":
		handleDelete(serverAddr, namespace, cleanArgs[1:])
	case "logs":
		handleLogs(serverAddr, namespace, cleanArgs[1:])
	default:
		fmt.Printf("Error: unknown command %q\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func handleApply(serverAddr, namespace string, args []string) {
	if len(args) == 0 {
		fmt.Println("Error: apply requires a file flag, e.g. -f <path>")
		os.Exit(1)
	}

	var filePath string
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" || args[i] == "--file" {
			if i+1 < len(args) {
				filePath = args[i+1]
				break
			}
		} else if strings.HasPrefix(args[i], "-f=") {
			filePath = strings.TrimPrefix(args[i], "-f=")
			break
		} else if strings.HasPrefix(args[i], "--file=") {
			filePath = strings.TrimPrefix(args[i], "--file=")
			break
		}
	}

	if filePath == "" {
		fmt.Println("Error: apply requires a file flag, e.g. -f <path>")
		os.Exit(1)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file %q: %v\n", filePath, err)
		os.Exit(1)
	}

	docs := splitYAML(data)
	for _, doc := range docs {
		var raw rawManifest
		if err := yaml.Unmarshal(doc, &raw); err != nil {
			fmt.Printf("Error parsing manifest: %v\n", err)
			continue
		}

		if raw.APIVersion == "" || raw.Kind == "" || raw.Metadata.Name == "" {
			fmt.Println("Error: manifest missing apiVersion, kind, or metadata.name")
			continue
		}

		ns := namespace
		if raw.Metadata.Namespace != "" {
			ns = raw.Metadata.Namespace
		}

		// Convert YAML doc to JSON for POST request
		var m map[string]interface{}
		if err := yaml.Unmarshal(doc, &m); err != nil {
			fmt.Printf("Error parsing manifest to map: %v\n", err)
			continue
		}
		jsonData, err := json.Marshal(m)
		if err != nil {
			fmt.Printf("Error converting manifest to JSON: %v\n", err)
			continue
		}

		url, err := getEndpoint(serverAddr, ns, raw.Kind, "")
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}

		resp, err := http.Post(url, "application/json", bytes.NewReader(jsonData))
		if err != nil {
			fmt.Printf("Error connecting to server: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
			fmt.Printf("%s/%s created/configured\n", strings.ToLower(raw.Kind), raw.Metadata.Name)
		} else {
			body, _ := io.ReadAll(resp.Body)
			var status types.StatusResponse
			_ = json.Unmarshal(body, &status)
			msg := status.Message
			if msg == "" {
				msg = string(body)
			}
			fmt.Printf("Error applying %s/%s: %s (Status: %d)\n", strings.ToLower(raw.Kind), raw.Metadata.Name, msg, resp.StatusCode)
		}
	}
}

func handleGet(serverAddr, namespace string, args []string) {
	if len(args) == 0 {
		fmt.Println("Error: get requires a resource type (e.g. pods, services, deployments)")
		os.Exit(1)
	}

	resourceType := args[0]
	var name string
	if len(args) > 1 {
		name = args[1]
	}

	url, err := getEndpoint(serverAddr, namespace, resourceType, name)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		os.Exit(1)
	}

	// If no name is specified, request Metav1.Table format
	if name == "" {
		req.Header.Set("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var status types.StatusResponse
		_ = json.Unmarshal(body, &status)
		msg := status.Message
		if msg == "" {
			msg = string(body)
		}
		fmt.Printf("Error: %s (Status: %d)\n", msg, resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		os.Exit(1)
	}

	if name != "" {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err == nil {
			fmt.Println(pretty.String())
		} else {
			fmt.Println(string(body))
		}
	} else {
		var table types.Table
		if err := json.Unmarshal(body, &table); err != nil {
			fmt.Println(string(body))
			return
		}

		if len(table.ColumnDefinitions) == 0 {
			fmt.Println("No resources found.")
			return
		}

		widths := make([]int, len(table.ColumnDefinitions))
		for i, col := range table.ColumnDefinitions {
			widths[i] = len(col.Name)
		}
		for _, row := range table.Rows {
			for i, cell := range row.Cells {
				cellStr := fmt.Sprintf("%v", cell)
				if len(cellStr) > widths[i] {
					widths[i] = len(cellStr)
				}
			}
		}

		for i, col := range table.ColumnDefinitions {
			fmt.Printf("%-*s   ", widths[i], strings.ToUpper(col.Name))
		}
		fmt.Println()

		for _, row := range table.Rows {
			for i, cell := range row.Cells {
				fmt.Printf("%-*s   ", widths[i], fmt.Sprintf("%v", cell))
			}
			fmt.Println()
		}
	}
}

func handleDelete(serverAddr, namespace string, args []string) {
	if len(args) < 2 {
		fmt.Println("Error: delete requires a resource type and resource name (e.g. delete pod my-pod)")
		os.Exit(1)
	}

	resourceType := args[0]
	name := args[1]

	url, err := getEndpoint(serverAddr, namespace, resourceType, name)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		os.Exit(1)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("%s/%s deleted\n", strings.ToLower(resourceType), name)
	} else {
		body, _ := io.ReadAll(resp.Body)
		var status types.StatusResponse
		_ = json.Unmarshal(body, &status)
		msg := status.Message
		if msg == "" {
			msg = string(body)
		}
		fmt.Printf("Error deleting %s/%s: %s (Status: %d)\n", strings.ToLower(resourceType), name, msg, resp.StatusCode)
	}
}

func handleLogs(serverAddr, namespace string, args []string) {
	var podName string
	var selector string
	var follow bool
	var tail string
	var all bool

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-f" || arg == "--follow" {
			follow = true
		} else if strings.HasPrefix(arg, "--tail=") {
			tail = strings.TrimPrefix(arg, "--tail=")
		} else if arg == "--tail" {
			if i+1 < len(args) {
				tail = args[i+1]
				i++
			}
		} else if arg == "-l" || arg == "--selector" {
			if i+1 < len(args) {
				selector = args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "-l=") {
			selector = strings.TrimPrefix(arg, "-l=")
		} else if strings.HasPrefix(arg, "--selector=") {
			selector = strings.TrimPrefix(arg, "--selector=")
		} else if arg == "--all" {
			all = true
		} else if strings.HasPrefix(arg, "-") {
			fmt.Printf("Error: unknown flag %q\n", arg)
			os.Exit(1)
		} else {
			if podName == "" {
				podName = arg
			} else {
				fmt.Printf("Error: multiple pod names specified (%s and %s)\n", podName, arg)
				os.Exit(1)
			}
		}
	}

	var podsToStream []string

	if podName != "" {
		podsToStream = []string{podName}
	} else if selector != "" || all {
		// fetch pods list
		url := fmt.Sprintf("%s/api/v1/namespaces/%s/pods", serverAddr, namespace)
		if selector != "" {
			url += "?labelSelector=" + strings.ReplaceAll(selector, " ", "")
		}
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("Error connecting to server: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("Error fetching pods: %s (Status: %d)\n", string(body), resp.StatusCode)
			os.Exit(1)
		}
		var podList types.PodList
		if err := json.NewDecoder(resp.Body).Decode(&podList); err != nil {
			fmt.Printf("Error decoding pods list: %v\n", err)
			os.Exit(1)
		}
		if len(podList.Items) == 0 {
			fmt.Println("No matching pods found.")
			return
		}
		for _, pod := range podList.Items {
			podsToStream = append(podsToStream, pod.Metadata.Name)
		}
	} else {
		fmt.Println("Error: logs requires a pod name, a selector (-l), or --all")
		printUsage()
		os.Exit(1)
	}

	colors := []string{
		"\033[32m", // Green
		"\033[33m", // Yellow
		"\033[34m", // Blue
		"\033[35m", // Magenta
		"\033[36m", // Cyan
		"\033[91m", // Light Red
		"\033[92m", // Light Green
		"\033[93m", // Light Yellow
		"\033[94m", // Light Blue
		"\033[95m", // Light Magenta
		"\033[96m", // Light Cyan
	}
	colorReset := "\033[0m"

	var wg sync.WaitGroup
	for idx, pod := range podsToStream {
		wg.Add(1)
		go func(pName string, colorIdx int) {
			defer wg.Done()

			colorPrefix := ""
			if len(podsToStream) > 1 {
				colorPrefix = colors[colorIdx%len(colors)] + "[" + pName + "] " + colorReset
			}

			url := fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s/log", serverAddr, namespace, pName)
			var params []string
			if follow {
				params = append(params, "follow=true")
			}
			if tail != "" {
				params = append(params, "tailLines="+tail)
			}
			if len(params) > 0 {
				url += "?" + strings.Join(params, "&")
			}

			resp, err := http.Get(url)
			if err != nil {
				fmt.Printf("Error connecting for %s: %v\n", pName, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				var status types.StatusResponse
				_ = json.Unmarshal(body, &status)
				msg := status.Message
				if msg == "" {
					msg = string(body)
				}
				fmt.Printf("Error getting logs for %s: %s (Status: %d)\n", pName, msg, resp.StatusCode)
				return
			}

			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadString('\n')
				if len(line) > 0 {
					if colorPrefix != "" {
						fmt.Print(colorPrefix + line)
					} else {
						fmt.Print(line)
					}
				}
				if err != nil {
					if err == io.EOF {
						break
					}
					fmt.Printf("Error reading stream for %s: %v\n", pName, err)
					break
				}
			}
		}(pod, idx)
	}
	wg.Wait()
}

func getEndpoint(serverAddr, namespace, kind, name string) (string, error) {
	kind = strings.ToLower(kind)
	switch kind {
	case "pod", "pods", "po":
		if name != "" {
			return fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s", serverAddr, namespace, name), nil
		}
		return fmt.Sprintf("%s/api/v1/namespaces/%s/pods", serverAddr, namespace), nil
	case "service", "services", "svc":
		if name != "" {
			return fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s", serverAddr, namespace, name), nil
		}
		return fmt.Sprintf("%s/api/v1/namespaces/%s/services", serverAddr, namespace), nil
	case "configmap", "configmaps", "cm":
		if name != "" {
			return fmt.Sprintf("%s/api/v1/namespaces/%s/configmaps/%s", serverAddr, namespace, name), nil
		}
		return fmt.Sprintf("%s/api/v1/namespaces/%s/configmaps", serverAddr, namespace), nil
	case "secret", "secrets":
		if name != "" {
			return fmt.Sprintf("%s/api/v1/namespaces/%s/secrets/%s", serverAddr, namespace, name), nil
		}
		return fmt.Sprintf("%s/api/v1/namespaces/%s/secrets", serverAddr, namespace), nil
	case "deployment", "deployments", "deploy":
		if name != "" {
			return fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments/%s", serverAddr, namespace, name), nil
		}
		return fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments", serverAddr, namespace), nil
	case "replicaset", "replicasets", "rs":
		if name != "" {
			return fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/replicasets/%s", serverAddr, namespace, name), nil
		}
		return fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/replicasets", serverAddr, namespace), nil
	default:
		return "", fmt.Errorf("unknown resource kind %q", kind)
	}
}

func splitYAML(data []byte) [][]byte {
	var docs [][]byte
	parts := strings.Split(string(data), "\n---")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			docs = append(docs, []byte(part))
		}
	}
	return docs
}

func printUsage() {
	fmt.Println(`Usage:
  lok8s <command> [args] [flags]

Commands:
  start                      Start the lok8s apiserver daemon (aliases: server, daemon)
  apply -f <file.yaml>       Apply resource configurations to apiserver
  get <resource> [name]      Retrieve resource details or list active resources
  delete <resource> <name>   Delete resources by type and name
  logs [pod-name] [flags]    Retrieve container logs for a pod

Global Flags:
  -s, --server <addr>        Apiserver address (default: http://localhost:8080)
  -n, --namespace <ns>       Target namespace (default: default)

Logs Flags:
  -f, --follow               Specify if logs should be streamed continuously
  -l, --selector <selector>  Filter pods by label selector for logs
  --all                      Stream logs from all pods in the namespace
  --tail <lines>             Number of lines from end of logs to display`)
}
