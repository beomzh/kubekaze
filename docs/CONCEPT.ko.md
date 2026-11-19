# 컨셉

> **kubekaze** (일본어 風, "바람"). [English](CONCEPT.md) | 한국어

## 한 줄 소개

터미널 중심의 쿠버네티스 동반 도구. **"무엇이 문제고 왜 그런가?"** 를 명령 한 번으로 답하고, `exec`, `logs -p`, 컨텍스트 전환 같은 일상의 번거로움을 없앱니다.

## 기존 도구와의 위치

| 도구 | 잘하는 것 | 이 프로젝트가 채우는 빈틈 |
|------|-----------|---------------------------|
| k9s | 대화형 탐색(TUI) | 파이프·스크립트·티켓 붙여넣기에 쓸 수 없음 |
| stern | 멀티 파드 로그 tail | 로그만 다룸, 원인 분석 없음 |
| krew 플러그인 | 작은 단일 기능 도구 모음 | 결과를 직접 이어 붙여야 함 |
| kubectl | 무엇이든 가능(날것) | 결론 하나에 도달하기까지 명령·플래그가 많음 |

설계 방향: **명령 입력 → 답 출력.** 상주 UI는 만들지 않습니다. 기본 출력은 사람이 읽기 좋게, `-o json`으로 기계가 읽게 합니다.

## 기능 축

### 1. 장애 대응 (Triage)

- `status`: 네임스페이스/클러스터에서 **비정상인 것만** 표시합니다(CrashLoopBackOff, Pending, ImagePullBackOff, 실패한 Job, NotReady 노드, 가득 차가는 PVC, 만료 임박 인증서). 정상은 숨깁니다.
- `why <kind>/<name>`: 한 오브젝트에 대한 상관 분석 타임라인 하나. 현재 상태, 이벤트, 직전 종료 사유(OOMKilled, exit code), 이전 컨테이너 로그 끝부분, owner 체인(Pod → ReplicaSet → Deployment), 최근 변경을 보여주고, 규칙 기반으로 순위를 매긴 **의심 원인**으로 마무리합니다.
- `events`: `kubectl get events`가 기본으로 해주지 않는, 실제로 필요한 방식으로 정렬한 이벤트.
  - `events -A`(전체 네임스페이스) 또는 `events -n a,b`(여러 개). 기본 정렬은 마지막 발생 시각 기준이며 최신이 맨 아래에 와서 터미널 하단에 가장 최근 문제가 보입니다.
  - `--sort time|count|type|namespace|object`, `--type Warning`, `--since 30m`, `--reason BackOff,FailedScheduling`, `--for pod/api`.
  - 반복되는 이벤트는 화면을 채우지 않도록 횟수로 합쳐 보여주며(`×37 over 12m`), `--no-collapse`로 원본을 봅니다.
  - `--summary`는 네임스페이스별 한 줄 요약(Warning 수, 상위 사유)으로 "어느 네임스페이스가 문제인지"를 빠르게 보여주고, `-w`는 새 이벤트를 실시간으로 따라갑니다.
  - 참고: 쿠버네티스는 이벤트를 기본 약 1시간만 보관하므로, 결과가 불완전할 수 있으면 그 사실을 알려줍니다.
- `changes --since 1h`: "깨지기 직전에 뭐가 바뀌었지?" (롤아웃, ConfigMap/Secret 변경은 해시만, 스케일링, 노드 이벤트).

### 2. 일상의 번거로움 제거

- `sh <pod>`: 이미지에 bash가 있는지 sh가 있는지 더는 추측하지 않습니다.
  1. 대상 해석: 파드 이름, `deploy/x`, 레이블 셀렉터 → Running 파드 선택. 컨테이너는 `kubectl.kubernetes.io/default-container` 어노테이션을 따르고, 없으면 물어봅니다.
  2. 후보 셸(`bash`, `sh`, `ash`, `busybox sh`)을 비대화형 exec로 차례로 확인하고 처음 성공한 것으로 접속합니다.
  3. 셸이 없는 이미지(distroless/scratch)면 설정 가능한 고정(pinned) 이미지로 **임시 디버그 컨테이너**(`kubectl debug` 방식)를 제안합니다.
  4. OpenShift 클러스터에서는 `oc` 없이도 `oc rsh`와 같은 기본 동작을 합니다.
- `logs`: stern 방식의 멀티 파드 tail에 더해, 컨테이너가 재시작했다면 이전 컨테이너 로그(`-p`)를 자동 포함하고 에러를 강조합니다.
- `rm`: `get | grep | awk | xargs kubectl delete`를 엮지 않고 파드(및 다른 리소스)를 한 번에 삭제합니다.
  - 이름 패턴, 레이블, 상태(`--status CrashLoopBackOff,Error,Evicted,Completed`), 경과 시간(`--older-than 2h`), 노드로 선택하거나 목록에서 대화형으로 고릅니다.
  - **항상 미리보기가 먼저**(삭제될 대상을 정확히 보여주는 dry run)이며 확인 후에만 삭제합니다. 많은 개수이거나 운영(prod) 컨텍스트이면 개수나 이름을 직접 입력해야 합니다. `--yes`는 스크립트용이며 안전 상한은 그대로 적용됩니다.
  - 오브젝트별 결과를 보고하고 실패를 조용히 넘기지 않습니다.
- `ctx`와 `ns`: kubectx/kubens처럼 클러스터 컨텍스트와 네임스페이스를 한 도구에서 전환합니다.
  - `ctx` / `ns`는 검색 가능한 목록을 열고, `ctx prod-eks`, `ns payments`, `ns -`(이전으로)도 바로 됩니다.
  - **OpenShift 프로젝트:** OpenShift에서는 `ns`가 프로젝트를 나열·전환합니다(`oc projects` / `oc project`에 해당). 같은 명령으로 동작합니다.
  - 운영(prod)으로 보이는 컨텍스트는 색으로 구분하고, 변경 명령은 확인을 받습니다.
  - 변경 명령은 실행 전에 현재 컨텍스트와 네임스페이스를 항상 보여줘서 어디에서 실행하는지 알 수 있게 합니다.

### 3. 멀티 클러스터, 모든 배포판

- 표준 kubeconfig와 client-go 인증을 그대로 사용하므로 EKS(`aws eks get-token`), AKS(kubelogin), GKE(gke-gcloud-auth-plugin), Rancher(프록시 kubeconfig), kubeadm/VM 클러스터, OpenShift가 같은 경로로 동작합니다. 자체 클라우드 인증은 구현하지 않습니다.
- Fan-out: `--context 'prod-*'` 또는 `--all-contexts`로 `status`/`why`를 병렬 실행하고 클러스터별로 합쳐 보여줍니다.
- 배포판별 힌트(관리형 노드그룹 이벤트, OpenShift SCC 거부, Rancher 프로젝트/네임스페이스 특성)는 감지되면 켜지는 선택적 규칙 팩으로 추가합니다.

## 원칙

1. **기본은 읽기 전용.** 상태를 바꾸는 것(`sh`, 디버그 컨테이너, 재시작)은 명시적으로, 그리고 눈에 띄게 표시합니다.
2. **에이전트·서버·텔레메트리 없음.** 기존 kubeconfig를 쓰는 단일 바이너리입니다.
3. **최소 권한.** 명령별 필요 RBAC를 문서화하고, 권한이 없으면 그 사실을 알리며 가능한 범위까지만 동작합니다.
4. **Secret 값은 출력하지 않음.** Secret 데이터와 Secret에서 온 env 값은 기본적으로 마스킹합니다.
5. **파이프 친화.** 안정적인 종료 코드와 `-o json`으로 스크립트·CI에서도 쓸 수 있습니다.
6. **규칙이 먼저, AI는 선택.**

## "AI 요약"이란

기본적으로 **꺼져 있는** 선택 기능입니다. 이미 수집되고 마스킹된 진단 결과(이벤트, 상태, 로그 끝부분)를 사용자가 설정한 LLM(호스팅 API 또는 Ollama 같은 로컬 모델)에 보내 쉬운 말로 설명하고 다음 조치를 제안받는 기능입니다. 로그와 이벤트에는 민감정보가 있을 수 있으므로 명시적으로 켜야 하고, 로컬 전용 모델을 지원해야 하며, 무엇이 전송되는지 `--dry-run`으로 미리 보여줘야 합니다. MVP는 규칙만 제공하고 AI는 나중에 끼우는 플러그인 계층으로 둡니다.

## MVP 범위

`status`, `why`, `events`, `sh`, `logs`, `rm`, `ctx`, `ns`. 나머지(`changes`, fan-out, 규칙 팩, AI)는 이후 마이너 릴리즈에서 추가합니다.

## 지원 플랫폼

런타임 의존성이 없는 단일 바이너리입니다(`kubectl`, `oc`, 셸이 필요 없음). 지원 대상:

| OS | amd64 | arm64 |
|----|:-----:|:-----:|
| Linux | 지원 | 지원 |
| macOS | 지원 | 지원 (Apple Silicon) |
| Windows | 지원 | 지원 |

Windows도 1급 지원 대상입니다. 대화형 `sh`는 Windows 콘솔 API로 raw 터미널 모드를 처리하고, 경로와 `KUBECONFIG` 구분자는 OS 규칙을 따르며, 색상은 지원되지 않으면 자연스럽게 꺼집니다. CI 매트릭스에서 세 OS 모두 테스트를 실행합니다.

## 기술

- Go, client-go, 단일 정적 바이너리. 독립 CLI와 krew `kubectl-` 플러그인 두 방식으로 배포합니다.
- 패키징·버전 정책은 [RELEASING.ko.md](RELEASING.ko.md), 보안 모델은 [../SECURITY.ko.md](../SECURITY.ko.md)를 참고하세요.

## 미정 사항

- 최종 이름과 CLI 명령어.
- 규칙 형식: 바이너리 내장으로 갈지, 처음부터 YAML 규칙 팩을 로드할지.
- 클러스터 내 구성요소 없이 `changes`를 어디까지 지원할지 (쿠버네티스 이벤트는 기본 약 1시간만 보관됨).
