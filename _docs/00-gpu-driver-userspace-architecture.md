# GPU 드라이버 유저스페이스 아키텍처

> libcuda.so는 왜 일반 라이브러리가 아닌가

## 배경

GPU 컨테이너 환경에서 가장 흔한 오해 중 하나는 `libcuda.so`를 일반 라이브러리처럼
컨테이너 이미지에 직접 설치할 수 있다고 생각하는 것이다.

## 일반 라이브러리 vs libcuda.so

| 구분 | 일반 라이브러리 (libssl, libz 등) | libcuda.so |
|------|----------------------------------|------------|
| 이미지에 포함 가능 여부 | O | **X** |
| 커널 의존성 | 없음 | nvidia.ko와 1:1 매칭 필수 |
| 노드마다 다를 수 있음 | X | **O** (드라이버 버전이 다르면 다름) |
| 런타임 주입 필요 | X | **O** |

## 커널 모듈과 유저스페이스의 관계

```
libcuda.so  ←── ioctl() ──→  nvidia.ko (커널 모듈)
   (유저스페이스)                  (커널)
```

`libcuda.so`는 커널의 NVIDIA 드라이버(`nvidia.ko`)와 **ioctl 시스템 콜**로 직접 통신한다.
둘 사이에는 내부 ABI(Application Binary Interface)가 존재하며,
**반드시 같은 드라이버 빌드에서 생성된 쌍**이어야 정상 동작한다.

### 호스트에서 드라이버 설치 시 흐름

```
드라이버 설치 (예: NVIDIA-Linux-x86_64-560.35.03.run)
    │
    ├─→ nvidia.ko         (커널 모듈로 로드)
    └─→ libcuda.so.560.35.03  (유저스페이스 라이브러리)
         이 둘은 동일 빌드의 쌍
```

## 컨테이너에서의 주입 메커니즘

```
호스트에서 드라이버 설치
    → nvidia.ko + libcuda.so 쌍으로 생성
                     │
      nvidia-container-toolkit이
      이 libcuda.so를 컨테이너에 bind mount로 주입
                     │
      컨테이너 안 CUDA 앱이 사용
```

### nvidia-container-toolkit의 역할

1. 컨테이너 런타임(containerd, cri-o 등)에 hook으로 등록
2. GPU 컨테이너 생성 시 호스트의 NVIDIA 라이브러리/디바이스 노드를 자동 마운트
3. 주입 대상:
   - `libcuda.so` — CUDA Driver API
   - `libnvidia-ml.so` — NVML (GPU 모니터링)
   - `libnvidia-ptxjitcompiler.so` — PTX JIT 컴파일러
   - `/dev/nvidia*` — GPU 디바이스 노드

## 컨테이너 이미지에 직접 넣으면 안 되는 이유

1. **호스트 드라이버 업그레이드 시 버전 불일치** → 크래시 또는 기능 오동작
2. **다른 노드에 스케줄링 시** 해당 노드의 드라이버 버전이 다르면 → 크래시
3. **이미지 이식성 상실** — 특정 드라이버 버전에 종속된 이미지가 됨

## GPU Operator에서의 처리

NVIDIA GPU Operator는 이 문제를 체계적으로 해결한다:

1. **state-driver**: 호스트에 NVIDIA 커널 모듈을 DaemonSet으로 설치
2. **state-container-toolkit**: nvidia-container-toolkit을 배포하여 런타임 주입 설정
3. **state-device-plugin**: `nvidia.com/gpu` 리소스를 Kubernetes에 등록

이 순서가 중요하다 — 드라이버가 먼저 설치되어야 toolkit이 주입할 라이브러리가 존재하고,
toolkit이 설정되어야 device plugin이 GPU를 할당받은 Pod에서 실제로 GPU를 사용할 수 있다.

## 정리

GPU 컨테이너 생태계가 복잡한 근본적 이유는 **단순히 라이브러리를 설치하는 문제가 아니라,
호스트 커널 드라이버와 매칭되는 유저스페이스를 런타임에 주입해야 하는 문제**이기 때문이다.
