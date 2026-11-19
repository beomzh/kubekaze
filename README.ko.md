# kubekaze

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Status](https://img.shields.io/badge/status-early%20development-orange)

[English](README.md) | 한국어

**무엇이 문제고 왜 그런지를 명령 한 번으로 알려주는, 터미널 중심의 쿠버네티스 동반 도구.**

> **상태: 초기 개발 중.** 아래 핵심 명령은 동작하며 kind 클러스터에서 확인했지만(일괄 삭제는 아직 단위 테스트로만 검증), 아직 태그된 릴리즈는 없습니다. 소스에서 빌드해 주세요([설치](#설치)). 설계 배경은 [docs/CONCEPT.ko.md](docs/CONCEPT.ko.md)를 참고하세요.

## 왜 만드나

- k9s는 탐색에는 좋지만 상주 UI라서 파이프로 넘기거나 티켓에 붙여넣기 어렵습니다.
- stern은 로그를 tail 하지만 원인을 설명해 주지는 않습니다.
- krew 플러그인은 작고 단일 목적이라 답을 직접 이어 붙여야 합니다.
- 일상의 번거로움: 이 이미지에 `bash`가 있나 `sh`가 있나? `logs -p` 였던가? 지금 어느 컨텍스트지?

## 명령

| 명령 | 기능 | 상태 |
|------|------|------|
| `kubekaze status` | 비정상인 것만 표시(파드, 워크로드, 노드, Job, PVC). 문제가 있으면 종료 코드 3 | 사용 가능 |
| `kubekaze why <pod\|deploy/x>` | 타임라인, 근거, 순위가 매겨진 의심 원인 | 사용 가능 |
| `kubekaze events -A --type Warning` | 전체 네임스페이스 이벤트를 시간순 정렬, `--summary`는 네임스페이스별 요약 | 사용 가능 |
| `kubekaze sh <pod\|deploy/x>` | 동작하는 셸(bash/sh/ash)을 찾거나 임시 디버그 컨테이너 사용 | 사용 가능 |
| `kubekaze logs <pod\|deploy/x>` | 멀티 파드 tail, 재시작한 컨테이너의 이전 로그 포함 | 사용 가능 |
| `kubekaze rm pods --status CrashLoopBackOff,Evicted` | 미리보기와 확인 후 일괄 삭제 | 사용 가능 |
| `kubekaze ctx [name\|-]` | 컨텍스트 전환 | 사용 가능 |
| `kubekaze ns [name\|-]` | 네임스페이스 전환, OpenShift는 프로젝트 | 사용 가능 |
| `kubekaze changes --since 1h` | 깨지기 직전에 무엇이 바뀌었나 | 계획 |
| `--all-contexts` / `--context 'prod-*'` | 여러 클러스터에 동시에 실행 | 계획 |
| AI 요약(선택, 로컬 모델 지원) | 쉬운 말로 설명 | 계획 |

예시:

```text
kubekaze status -A
kubekaze why deploy/api
kubekaze sh deploy/api -c sidecar
kubekaze sh api-7d9f -- ls -l /app
kubekaze logs deploy/api -f --since 10m
kubekaze events -A --summary
kubekaze rm pods -A --status Evicted --dry-run
```

종료 코드: `0` 정상, `1` 오류, `3` 문제 발견(`status`, `why`). `sh`는 원격 명령의 종료 코드를 그대로 전달합니다.

공통 플래그: `--kubeconfig`, `--context`, `-n/--namespace`, `-A/--all-namespaces`, `-o text|json`.

## 설치

아직 게시된 릴리즈가 없습니다. 소스에서 빌드해 주세요(Go 1.26 이상):

```bash
git clone https://github.com/beomzh/kubekaze.git
cd kubekaze
make build        # ./bin/kubekaze 생성
```

Linux, macOS, Windows(amd64, arm64) 릴리즈 바이너리는 Releases 페이지에 게시될 예정입니다. [docs/RELEASING.ko.md](docs/RELEASING.ko.md)를 참고하세요.

## 지원 환경

표준 kubeconfig와 client-go 인증을 사용하므로 VM/kubeadm 클러스터, EKS, AKS, GKE, OpenShift, Rancher 관리 클러스터에서 같은 도구로 동작합니다. 멀티 클러스터 fan-out(`--context 'prod-*'`)도 계획 중입니다.

## 지원 플랫폼

Linux, macOS, Windows를 amd64와 arm64 모두 지원하는 단일 바이너리입니다(`kubectl`, `oc`, 셸 불필요).

## 원칙

기본 읽기 전용 · 에이전트/서버/텔레메트리 없음 · 최소 권한 RBAC · Secret 값 미출력 · 파이프 친화(`-o json`) · 규칙 우선, AI는 선택.

## 보안

[SECURITY.ko.md](SECURITY.ko.md)를 참고하세요. 취약점은 GitHub의 "Report a vulnerability"로 비공개 제보해 주세요.

## 문서

- [컨셉](docs/CONCEPT.ko.md)
- [릴리즈와 패키징](docs/RELEASING.ko.md)
- [보안 정책](SECURITY.ko.md)

## 라이선스

[Apache License 2.0](LICENSE)으로 배포됩니다. [NOTICE](NOTICE)를 참고하세요.

Kubernetes는 The Linux Foundation의 등록 상표입니다. 이 프로젝트는 Kubernetes 프로젝트, Red Hat, AWS, Microsoft, Google, SUSE와 제휴하거나 보증을 받지 않았습니다.
