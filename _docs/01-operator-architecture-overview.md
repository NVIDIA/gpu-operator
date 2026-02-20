# GPU Operator 아키텍처 개요

> NVIDIA GPU Operator의 전체 구조와 동작 원리

## 프로젝트 개요

NVIDIA GPU Operator는 Kubernetes 클러스터에서 GPU 노드를 운영하는 데 필요한
모든 NVIDIA 소프트웨어 컴포넌트의 라이프사이클을 자동화하는 Kubernetes Operator이다.

### 핵심 가치

- GPU 노드에 **표준 OS 이미지**만 사용 (커스텀 이미지 불필요)
- 드라이버, 툴킷, 디바이스 플러그인 등을 **컨테이너로 배포/관리**
- 클라우드 환경에서 GPU 노드의 동적 프로비저닝에 적합

## CRD (Custom Resource Definitions)

### 1. ClusterPolicy (v1) — 주요 CRD

클러스터 전체의 GPU 소프트웨어 스택 상태를 선언한다.

```yaml
apiVersion: nvidia.com/v1
kind: ClusterPolicy
metadata:
  name: cluster-policy
spec:
  driver:         # NVIDIA 커널 드라이버
  toolkit:        # Container Toolkit
  devicePlugin:   # Kubernetes Device Plugin
  dcgm:           # DCGM (Data Center GPU Manager)
  dcgmExporter:   # DCGM 메트릭 익스포터
  migManager:     # Multi-Instance GPU 관리
  gfd:            # GPU Feature Discovery
  # ... 기타 컴포넌트
```

### 2. NVIDIADriver (v1alpha1) — 세분화된 드라이버 관리

노드 풀별로 다른 드라이버 설정을 적용할 수 있는 CRD이다.

```yaml
apiVersion: nvidia.com/v1alpha1
kind: NVIDIADriver
metadata:
  name: gpu-driver
spec:
  driverType: gpu            # gpu | vgpu | vgpu-host-manager
  kernelModuleType: open     # auto | open | proprietary
  usePrecompiled: false
```

## 컨트롤러 (Reconcilers)

### 1. ClusterPolicyReconciler

- `ClusterPolicy` 리소스와 `Node` 라벨을 감시
- **상태 머신(State Machine)** 기반으로 모든 operand를 순서대로 배포
- 준비 안 된 상태가 있으면 5초마다 재시도

### 2. NVIDIADriverReconciler

- `NVIDIADriver` CR을 reconcile
- 노드 풀별 드라이버 DaemonSet 관리

### 3. UpgradeReconciler

- GPU 드라이버 DaemonSet의 롤링 업그레이드 관리
- `nvidia.com/gpu` 리소스를 사용하는 Pod만 필터링하여 처리

## 상태 머신 (State Machine)

ClusterPolicy 컨트롤러는 고정된 순서로 상태(operand)를 처리한다:

```
1.  pre-requisites          ─ RuntimeClass 등 전제 조건
2.  state-operator-metrics  ─ Operator 메트릭 수집
3.  state-driver            ─ NVIDIA 커널 드라이버 (DaemonSet)
4.  state-container-toolkit ─ nvidia-container-toolkit
5.  state-operator-validation ─ Operator 검증
6.  state-device-plugin     ─ Kubernetes GPU Device Plugin
7.  state-mps-control-daemon ─ MPS (Multi-Process Service)
8.  state-dcgm              ─ DCGM 독립 컴포넌트
9.  state-dcgm-exporter     ─ DCGM 메트릭 익스포터
10. gpu-feature-discovery   ─ GPU Feature Discovery
11. state-mig-manager       ─ MIG 파티셔닝 관리
12. state-node-status-exporter ─ 노드 상태 익스포터
```

### Sandbox 워크로드 상태 (VM/vGPU)

```
13. state-vgpu-manager
14. state-vgpu-device-manager
15. state-sandbox-validation
16. state-vfio-manager
17. state-sandbox-device-plugin
18. state-kata-manager
19. state-cc-manager
```

### 상태 순서가 중요한 이유

```
driver → toolkit → device-plugin

1. 드라이버가 먼저 설치되어야 → libcuda.so가 존재
2. toolkit이 설정되어야      → 컨테이너에 주입 가능
3. device-plugin이 등록되어야 → Pod에 GPU 할당 가능
```

## Assets 구조

각 상태는 `assets/` 디렉토리의 YAML 매니페스트에 매핑된다:

```
assets/
├── state-driver/
│   ├── 0100_service_account.yaml
│   ├── 0200_role.yaml
│   ├── 0300_cluster_role.yaml
│   ├── 0400_rolebinding.yaml
│   └── 0500_daemonset.yaml
├── state-container-toolkit/
├── state-device-plugin/
├── state-dcgm/
├── state-dcgm-exporter/
├── gpu-feature-discovery/
├── state-mig-manager/
└── ...
```

번호 접두사(`0100_`, `0200_`)는 리소스 생성 순서를 보장한다.

## 워크로드 타입별 노드 라벨

GPU Operator는 노드의 워크로드 타입에 따라 다른 컴포넌트를 배포한다:

| 워크로드 타입 | 배포 컴포넌트 |
|-------------|-------------|
| **container** (기본) | driver, toolkit, device-plugin, dcgm, dcgm-exporter, gfd, node-status-exporter |
| **vm-passthrough** | sandbox-device-plugin, vfio-manager, kata-manager, cc-manager |
| **vm-vgpu** | sandbox-device-plugin, vgpu-manager, vgpu-device-manager, cc-manager |

## GPU 노드 감지

Node Feature Discovery(NFD)가 GPU 노드에 다음 라벨을 부여하면 Operator가 감지한다:

```
feature.node.kubernetes.io/pci-10de.present=true
```

`10de`는 NVIDIA의 PCI 벤더 ID이다.

## 디렉토리 구조 요약

```
gpu-operator/
├── api/          # CRD 타입 정의 (Go)
├── assets/       # 각 operand의 Kubernetes 매니페스트
├── cmd/          # 바이너리 진입점 (gpu-operator, nvidia-validator 등)
├── controllers/  # Reconciler/Controller 로직
├── internal/     # 내부 라이브러리 (state, conditions, render 등)
├── deployments/  # Helm 차트
├── config/       # Kustomize/kubebuilder 설정
├── validator/    # GPU 노드 검증 로직
└── vendor/       # Go 의존성
```
