# lok8s ⚡

**The Containerless Local Kubernetes Runtime.**

`lok8s` (gesprochen: *Local-K8s*) ist ein extrem leichtgewichtiger, in Go geschriebener Prozess-Supervisor für den "Inner Dev Loop". Er verhält sich nach außen wie ein echter Kubernetes API-Server, nutzt intern jedoch **keine Container-Daemons** (wie Docker oder containerd).

Anstatt Ressourcen für schwere Virtualisierungsschichten zu verschwenden, führt `lok8s` deine Workloads ballastfrei, isoliert und lokal aus – entweder als nativen Betriebssystem-Prozess oder in einer hochsicheren WebAssembly-Sandbox.

---

## 🎯 Das Problem

Die lokale Cloud-Native-Entwicklung ist zu schwergewichtig geworden. Tools wie `minikube`, `kind` oder Docker Desktop zwingen Entwickler in einen langsamen Dev-Loop:

1. Code kompilieren.
2. Container-Image bauen.
3. Image in die lokale Registry pushen.
4. Alten Pod zerstören, neuen hochfahren.

Das kostet Zeit, CPU-Zyklen und Akkulaufzeit.

## 💡 Die Lösung: Dual-Engine Architektur

`lok8s` eliminiert diesen Overhead komplett. Es mockt die K8s-Control-Plane und übersetzt Standard-Manifeste in rasend schnelle lokale Prozesse. Du hast die Wahl zwischen zwei Ausführungs-Engines:

1. **Native OS Engine (`os/exec`):** Führt deine kompilierten lokalen Binaries (Go, Rust, Node) direkt als Kindprozesse aus. Perfekt für maximalen Speed und vollen Systemzugriff während der Entwicklung.
2. **WebAssembly Engine (`wazero`):** Lädt `.wasm`-Module als Byte-Array und führt sie in einer sicheren, plattformunabhängigen In-Memory-Sandbox aus. Zero-Overhead-Isolation ohne CGO.

---

## ✨ Features

* **Sub-Millisecond Boot Times:** Kein Image-Pull, kein Container-Overhead. Dein Code startet in dem Moment, in dem du `kubectl apply` drückst.
* **100% K8s API Compatible:** Nutze deine gewohnten Tools. `kubectl`, `helm` und Kustomize funktionieren out-of-the-box gegen `localhost:8080`.
* **Zero Configuration / Fallback to Image:** Du kannst deine unmodifizierten Produktions-Manifeste direkt deployen! `lok8s` löst den Bildnamen (`image`) automatisch auf und sucht nach einer passenden lokalen Binärdatei in deinem Suchpfad (`PATH`), deinem aktuellen Verzeichnis oder unter `./bin/`.
* **Multi-Pod Colorized Log Streaming:** Streame Logs von mehreren Pods gleichzeitig mit `kubectl logs -l ...` oder `lok8s logs -l ...`. Zeilen werden automatisch farblich nach Pod markiert.
* **Privacy & Local by Design:** Komplett offline-fähig. Keine Daten verlassen dein Host-System, kein Remote-Cluster erforderlich.
* **Smart Localhost Routing:** `lok8s` fängt K8s-Services ab und weist deinen nativen Prozessen dynamisch freie Ports zu.

---

## 🚀 Quick Start

### 1. Installation

Lade dir das vorkompilierte Binary herunter oder baue es direkt aus dem Quellcode:

```bash
git clone https://github.com/megawron/lok8s.git
cd lok8s
go build -o lok8s main.go
./lok8s start
```

*Der lok8s API-Server lauscht nun auf `localhost:8080`.*

### 2. Standard-Manifeste direkt ausführen (Zero-Config)

Du kannst deine produktionsbereiten Standard-Manifeste ohne Änderungen deployen. `lok8s` sucht nach einer ausführbaren Datei auf dem Host, die dem Bildnamen entspricht (z. B. `my-go-service` für `image: my-go-service:v1.0.0`):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-fast-backend
spec:
  containers:
  - name: app
    image: my-go-service:v1.0.0
    env:
    - name: DB_HOST
      value: "localhost:5432"
```

Stelle sicher, dass `my-go-service` kompiliert und im aktuellen Verzeichnis, unter `./bin/` oder in deinem System-`PATH` verfügbar ist. Wende das Manifest mit Standard-Tools an:

```bash
kubectl apply -f pod.yaml
```

### 3. Anpassung über Annotationen (Optional)

Wenn du den Ausführungspfad explizit angeben willst oder WebAssembly nutzen möchtest, kannst du dies über Annotations steuern:

#### Native Binaries
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-fast-backend
  annotations:
    lok8s.io/executable-path: "./bin/my-go-service"
spec:
  containers:
  - name: app
```

#### WebAssembly Module
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: secure-wasm-worker
  annotations:
    lok8s.io/wasm-module: "./bin/worker.wasm"
spec:
  containers:
  - name: worker
```

`lok8s` liest das Manifest, greift den kompilierten Code und startet den Prozess in Millisekunden. Stdout und Stderr werden sauber in dein Supervisor-Terminal geroutet.

---

## 🏗️ Architektur & Tech Stack

`lok8s` ist zu 100% in Go geschrieben.

* **Control Plane Simulation:** Bietet eine Kubernetes-kompatible REST-API an, sodass du wie gewohnt mit `kubectl`, `helm` oder `kustomize` arbeiten kannst.
* **Process Management:** Handhabt den Lebenszyklus von Kindprozessen ressourcenschonend auf Systemebene.
* **Wasm Runtime:** Integriert [wazero](https://wazero.io/) für absolute Speichersicherheit und CGO-freie WebAssembly-Ausführung.

## 🤝 Contributing

Wir freuen uns über Pull Requests! Egal ob du das Fuzz-Testing erweitern, die Netzwerk-Illusion robuster machen oder neue Edge-Cases abfangen willst – schau dir die offenen Issues an.

1. Fork the repo
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.
