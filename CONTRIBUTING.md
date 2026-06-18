# Contributing to lok8s

First off, thank you for taking the time to contribute! 🎉

`lok8s` is built to make local Kubernetes development lightning-fast, lightweight, and fun. We welcome contributions of all kinds: bug reports, feature requests, documentation improvements, and code submissions.

---

## Code of Conduct

We aim to foster an open, welcoming, and inclusive community. Please be respectful, constructive, and friendly in all communications and contributions.

---

## How Can I Contribute?

### 1. Reporting Bugs
* Check the existing issues to ensure the bug hasn't already been reported.
* Open a new issue with a clear title and description.
* Include steps to reproduce the issue, the expected behavior, and logs if possible.

### 2. Suggesting Enhancements
* Check if the feature has already been discussed or requested.
* Open an issue explaining the proposed change, why it would be useful, and how it should work.

### 3. Submitting Pull Requests (PRs)
* Fork the repository and create your branch from `main`.
* Write clean, documented, and well-tested code.
* Ensure all existing tests pass and add new tests for your changes.
* Keep commit messages descriptive and clear.

---

## Local Development Setup

To build and run `lok8s` locally, you need:
* **Go 1.22+** installed on your host system.
* A standard **Kubernetes client** (like `kubectl`) to interact with the API.

### 1. Clone your fork and download dependencies
```bash
git clone https://github.com/<your-username>/lok8s.git
cd lok8s
go mod download
```

### 2. Run the tests
Make sure everything works correctly before making changes:
```bash
go test ./...
```

### 3. Build and run the binary
Compile the unified binary:
```bash
go build -o lok8s main.go
```

Start the local `lok8s` apiserver:
```bash
./lok8s start
```

In another terminal, direct `kubectl` to your local `lok8s` daemon:
```bash
kubectl config set-cluster lok8s --server=http://localhost:8080
kubectl config use-context lok8s
```

---

## Project Architecture

A quick overview of the directory structure:
* [/api](file:///c:/Users/mgawron/Documents/Projekte/lok8s/api): HTTP server and handlers exposing the mock Kubernetes REST API.
* [/engine](file:///c:/Users/mgawron/Documents/Projekte/lok8s/engine): Pod engines implementing the execution of processes (Native OS processes or sandboxed WebAssembly via `wazero`).
* [/manifest](file:///c:/Users/mgawron/Documents/Projekte/lok8s/manifest): YAML parsers translating Kubernetes manifests to internal models.
* [/network](file:///c:/Users/mgawron/Documents/Projekte/lok8s/network): TCP reverse proxies, routing, and dynamic port pool manager.
* [/store](file:///c:/Users/mgawron/Documents/Projekte/lok8s/store): Data persistence layers using `bbolt` database.
* [/volume](file:///c:/Users/mgawron/Documents/Projekte/lok8s/volume): Logic for projected volumes, configmaps, and secrets.
