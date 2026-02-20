# 컨테이너 런타임 GPU 라이브러리 주입

> nvidia-container-toolkit이 GPU 라이브러리를 컨테이너에 주입하는 메커니즘

## 문제 정의

GPU 워크로드를 컨테이너에서 실행하려면 다음이 필요하다:

1. **NVIDIA 디바이스 노드** (`/dev/nvidia0`, `/dev/nvidiactl`, `/dev/nvidia-uvm`)
2. **유저스페이스 라이브러리** (`libcuda.so`, `libnvidia-ml.so` 등)
3. **적절한 cgroup 권한**

이들은 호스트의 드라이버 버전에 종속되므로 컨테이너 이미지에 미리 포함할 수 없다.

## nvidia-container-toolkit 동작 흐름

```
┌─────────────────────────────────────────────────┐
│ Kubernetes                                       │
│  ┌──────────┐    ┌───────────────┐              │
│  │ kubelet  │───→│ Container     │              │
│  │          │    │ Runtime       │              │
│  └──────────┘    │ (containerd)  │              │
│                  └───────┬───────┘              │
│                          │                      │
│              ┌───────────▼──────────┐           │
│              │ nvidia-container-    │           │
│              │ runtime (OCI hook)   │           │
│              └───────────┬──────────┘           │
│                          │                      │
│              ┌───────────▼──────────┐           │
│              │ nvidia-container-cli │           │
│              │  • bind mount libs   │           │
│              │  • mount /dev/nvidia*│           │
│              │  • set cgroup perms  │           │
│              └──────────────────────┘           │
└─────────────────────────────────────────────────┘
```

### 주입 단계

1. **Pod 생성 요청**: `nvidia.com/gpu` 리소스를 요청하는 Pod이 스케줄링됨
2. **Device Plugin**: GPU 디바이스를 할당하고 환경 변수 설정
3. **컨테이너 런타임 Hook**: nvidia-container-runtime이 OCI 런타임 hook으로 개입
4. **라이브러리 마운트**: 호스트의 NVIDIA 라이브러리를 컨테이너에 bind mount
5. **디바이스 노드 마운트**: `/dev/nvidia*` 디바이스를 컨테이너에 노출
6. **컨테이너 시작**: CUDA 앱이 주입된 라이브러리와 디바이스를 사용

## 주입되는 라이브러리 목록

| 라이브러리 | 역할 |
|-----------|------|
| `libcuda.so` | CUDA Driver API (커널 모듈과 직접 통신) |
| `libnvidia-ml.so` | NVML — GPU 모니터링/관리 API |
| `libnvidia-ptxjitcompiler.so` | PTX → SASS JIT 컴파일러 |
| `libnvidia-opencl.so` | OpenCL ICD |
| `libnvidia-nvvm.so` | NVVM IR 컴파일러 |
| `libnvidia-cfg.so` | GPU 설정 관리 |
| `libnvidia-allocator.so` | 메모리 할당 |

## 주입되는 디바이스 노드

| 디바이스 | 역할 |
|---------|------|
| `/dev/nvidia0` (..N) | 각 GPU 디바이스 |
| `/dev/nvidiactl` | NVIDIA 제어 디바이스 |
| `/dev/nvidia-uvm` | Unified Virtual Memory |
| `/dev/nvidia-uvm-tools` | UVM 디버깅 도구 |
| `/dev/nvidia-modeset` | 디스플레이 모드 설정 |

## GPU Operator에서의 배포

GPU Operator의 **state-container-toolkit** 상태가 이 컴포넌트를 배포한다:

```
assets/state-container-toolkit/
├── 0100_service_account.yaml
├── 0200_role.yaml
├── 0300_cluster_role.yaml
├── 0400_rolebinding.yaml
├── 0500_cluster_role_binding.yaml
└── 0600_daemonset.yaml     ← toolkit DaemonSet
```

DaemonSet이 각 GPU 노드에서 실행되며:
1. 호스트의 컨테이너 런타임 설정을 수정
2. nvidia-container-runtime을 기본 OCI 런타임으로 등록
3. 노드 재부팅 없이 설정 적용

## CUDA Runtime API vs Driver API

```
┌──────────────────────────────┐
│        사용자 CUDA 코드        │
│  (cudaMalloc, cudaMemcpy)    │
└──────────────┬───────────────┘
               │
┌──────────────▼───────────────┐
│     libcudart.so             │  ← CUDA Runtime API
│     (CUDA Runtime Library)   │     이미지에 포함 가능!
└──────────────┬───────────────┘
               │
┌──────────────▼───────────────┐
│     libcuda.so               │  ← CUDA Driver API
│     (CUDA Driver Library)    │     호스트에서 주입 필수!
└──────────────┬───────────────┘
               │ ioctl()
┌──────────────▼───────────────┐
│     nvidia.ko                │  ← 커널 모듈
│     (Kernel Module)          │
└──────────────────────────────┘
```

**중요한 구분:**
- `libcudart.so` (CUDA Runtime API) → 컨테이너 이미지에 **포함 가능** (nvidia/cuda 베이스 이미지에 이미 포함)
- `libcuda.so` (CUDA Driver API) → 호스트에서 **주입 필수** (드라이버 버전 종속)
