# 변경 이력

[English](CHANGELOG.md) | 한국어

주요 변경 사항을 여기에 기록합니다. 형식은 [Keep a Changelog](https://keepachangelog.com/ko/), 버전은 [SemVer](https://semver.org/lang/ko/)를 따릅니다.

## [Unreleased]

## [0.2.5] - 2026-11-27 (미배포, 태그 없음, 다음 마이너 릴리즈인 0.3.0에 포함 예정)

### 추가
- `kubekaze changes [--since 1h] [-n|-A]`: 깨지기 직전에 무엇이 바뀌었는지 오래된 순으로 보여 줍니다. Deployment·StatefulSet·DaemonSet 롤아웃(Deployment는 이미지와 env·리소스·명령·probe 차이, 환경 변수 값은 출력하지 않음), 생성된 객체, ConfigMap·Secret·Service·Ingress·워크로드 spec의 수정과 수정한 field manager, Helm 릴리스 revision, 오토스케일링, 노드의 재부팅·NotReady·cordon 이벤트. `-o json`과 `--since` 탭 완성을 지원합니다. 삭제된 객체와 이전 값은 보여 줄 수 없습니다.

## [0.2.4] - 2026-11-27 (미배포, 태그 없음, 다음 마이너 릴리즈인 0.3.0에 포함 예정)

### 변경
- `events` / `why`: 일회성 ID(컨테이너·레이어 ID, 재시작 시도 번호)만 다른 이벤트는 한 줄로 합치고 최신 메시지를 보여 줍니다. 컨테이너가 다르거나 이미지 digest가 다르면 합치지 않습니다.
- `why node`: 재시작 몰림에서 생략한 수가 *컨테이너* 수임을 표시합니다.

### 추가
- `why node` / `why nodes`: 노드의 컨테이너 스토리지 장애(파드 상태의 I/O 에러 또는 overlay 마운트 실패)를 노드 문제로 보고합니다.

## [0.2.3] - 2026-11-27 (미배포, 태그 없음, 다음 마이너 릴리즈인 0.3.0에 포함 예정)

### 변경
- `why node`: 재시작 몰림에 마지막 종료 시각(날짜·시간·시간대)과 컨테이너별 exit code·사유·시각을 보여 주어, `kubectl`을 더 치지 않고 해당 시각의 노드 로그를 찾을 수 있습니다.
- `why node`: 24시간 넘게 반복이 없는 재시작 몰림이나 재부팅은 "Past events"에 따로 보여 주고 현재 문제로 치지 않습니다(노드는 정상이고 종료 코드 3이 아닙니다). `why nodes`는 정상 노드의 지난 사건을 한 줄씩 알려 주며, `-o json`에는 `"past": true`가 붙습니다.
- `why node`: 컨트롤 플레인 구성요소가 함께 재시작한 경우, 서로 관계없는 파드라는 말 대신 API 서버와 etcd를 가리키는 안내를 보여 줍니다.

## [0.2.2] - 2026-11-27 (미배포, 태그 없음, 다음 마이너 릴리즈인 0.3.0에 포함 예정)

### 추가
- README, 보안 정책, 컨셉 문서의 중국어(간체)·일본어판을 추가하고 모든 언어판에서 서로 링크했으며, README는 릴리즈 아카이브에도 포함합니다. CHANGELOG와 릴리즈 가이드는 영어·한국어로 유지하고, 명령 출력과 `--help`는 영어입니다.

## [0.2.1] - 2026-11-20 (미배포, 태그 없음, 다음 마이너 릴리즈인 0.3.0에 포함 예정)

### 변경
- `completion install --alias kk`와 도움말이 `--alias`는 이미 있는 이름에 대해 탭 완성만 연결할 뿐 `kk` 명령을 만들지는 않는다는 점과, 정의하는 방법(`alias kk=kubekaze` 또는 심볼릭 링크)을 알려줍니다. 루트 도움말의 "also for an alias" 문구가 별칭을 만들어 주는 것처럼 읽히던 점을 고쳤습니다.

## [0.2.0] - 2026-11-20

미배포였던 0.1.3~0.1.8을 모두 포함합니다: `ns` 요약, `ls`, `top`, 노드 재시작 신호, 실제 OpenShift 클러스터 테스트에서 찾은 수정.

### 추가
- `why node`: 컨테이너 재시작 이력을 진단에 반영합니다. 서로 관계없는 여러 파드의 컨테이너가 10분 안에 함께 재시작했으면(최근 7일) 노드 수준 사건으로 보고하고, 노드 안에서 볼 것(`last -x reboot`, `journalctl --list-boots`, `coredumpctl list`, `dmesg -T`)을 안내합니다. 쿠버네티스는 커널·런타임 크래시나 코어 덤프를 볼 수 없기 때문입니다. 노드에 이미 문제가 있을 때는 최근 7일에 재시작이 10회 이상인 컨테이너를 많은 순으로 보여 주며, 정상 노드를 비정상으로 만들지는 않습니다.
- `kubekaze top`: metrics API(metrics-server)를 읽는 읽기 전용 사용량 스냅샷. 노드의 CPU·메모리 사용량과 allocatable 대비 비율(높은 순, 80% 초과는 빨강), 현재 네임스페이스(`-A`면 전체)의 `--sort cpu|memory` 상위 파드(`--limit`, 기본 10)를 limit 대비 비율과 함께 보여 주어 스로틀링·OOM 직전 파드가 눈에 띕니다. `top ns`(또는 `--by-namespace`)는 네임스페이스별로 합산합니다. `-w/--watch [--interval 5s]`는 터미널에서는 제자리에서 다시 그리고, 아니면 스냅샷을 순서대로 출력합니다. Ctrl-C는 종료 코드 130입니다. `-o json`과 `--sort`, `--limit`, `--interval` 탭 완성을 지원합니다.
- metrics-server가 없거나 metrics API 권한이 없으면 무엇을 설치하거나 어떤 RBAC가 필요한지 한 줄로 알리고 종료 코드 1로 끝납니다.
- `kubekaze ls`: 현재 네임스페이스(`-n`, `-A`)를 한 화면으로 요약합니다. Deployment·StatefulSet·DaemonSet(준비/원하는 수), Job, CronJob, Service, Ingress, OpenShift Route(클러스터에 있을 때만), PVC 상태, ConfigMap·Secret 이름. 비정상이 먼저 나오고, 많은 정상 항목은 개수로 요약하며, 읽을 권한이 없는 종류는 건너뛴 것으로 표시합니다.
- `kubekaze ls <종류> [이름...]`은 한 종류만 자세히 보여줍니다(`deploy`, `sts`, `ds`, `job`, `cronjob`, `svc`, `ingress`, `route`, `pvc`, `cm`, `secret`). 탭 완성으로 종류와 객체 이름을 제안하며 `-o json`을 지원합니다.
- Secret은 이름, 타입, 키 개수만 표시하고 데이터는 절대 출력하지 않습니다.
- `ns`: 대화형 선택기가 각 네임스페이스(또는 OpenShift 프로젝트) 옆에 파드 수, 비정상 파드 수(`status`와 같은 기준), 최근 1시간의 Warning 이벤트 수를 보여 줍니다. 파드와 이벤트는 네임스페이스마다가 아니라 클러스터 전체를 한 번씩만 조회하며, 필터링은 계속 이름에 대해 동작합니다. 클러스터 전체 조회 권한이 없으면 안내 메시지와 함께 이름만 있는 목록을 보여 줍니다(`--no-summary`는 조회를 건너뜁니다).
- `ns --list`(`-l`), `ns -o json`: 같은 요약을 비대화형으로 출력해 파이프와 스크립트에서 쓸 수 있습니다. 파이프로 연결한 평범한 `ns`는 계속 이름만 출력합니다.
- `why node`: 노드의 네트워크 에이전트(ovnkube-node, calico-node, cilium, aws-node, flannel, multus 등) 장애를 `ContainerCreating`에 멈춘 파드들의 원인으로 보고하고 다음에 볼 파드를 안내합니다. 5분 넘게 멈춘 파드가 여러 개면 노드 수준 문제로 보고합니다.

### 변경
- 정상 노드에 대한 `why node`는 쿠버네티스 API만 확인했다는 점과 노드 안에서 볼 곳(`coredumpctl list`, `dmesg -T`)을 알려줍니다.
- `why node`: `Rebooted` 이벤트를 2시간이 아니라 7일 동안 보여 줍니다(2시간이 지나면 순위는 낮춤). 재부팅은 저절로 사라지는 상태가 아니라 노드의 과거 사실이기 때문입니다.
- `-A` 없는 `status`는 현재 네임스페이스와 노드만 점검했다고 알려줍니다. 거기서의 "정상"은 클러스터 나머지에 대해 아무것도 말해 주지 않기 때문입니다.

### 수정
- `sh --debug -- <명령>`: `--debug`가 조용히 무시되고 명령이 애플리케이션 컨테이너에서 실행되던 문제를 고쳤습니다. 디버그 컨테이너는 대화형이라서 이제 메시지와 함께 거부합니다.
- `why <pod>`: 노드의 컨테이너 스토리지 문제(예: `creating overlay mount ... input/output error`)로 생긴 `CreateContainerError`를 "ConfigMap/Secret 없음"으로 안내하던 문제를 고쳤습니다. 이제 노드 파일시스템 장애로 보고하고 노드에서 확인할 것을 안내합니다. 실제로 ConfigMap/Secret이 없는 경우는 그대로 안내합니다.
- `why` / `events`: 1000자가 넘기도 하는 긴 이벤트·근거 메시지는 표에서 240자로 줄입니다. `-o json`에는 전체가 남습니다.
- `why <pod>`: 로그 줄은 읽어 온 컨테이너의 근거로만 인용하고, 같은 파드의 다른 컨테이너 근거로는 인용하지 않습니다.
- `completion --alias`(bash): `kk`가 셸 alias로도 정의돼 있지 않으면 스크립트가 `kk __complete`를 실행하다 실패해 파일 이름 완성으로 떨어지던 문제를 고쳤습니다. 이제 `kubekaze`를 직접 호출합니다.
- `why node` / `status`: heartbeat가 몇 분 지난 Ready 노드를 "응답 없음"으로 보고하던 문제를 고쳤습니다. kubelet은 노드 상태를 약 5분마다만 갱신하므로(생존 여부는 Lease로 확인) 기준을 10분으로 늘렸습니다.
- `why node`: 2시간보다 오래된 경고(OpenShift는 이벤트를 며칠 보관)는 더 이상 원인으로 보지 않고, `ImageGCFailed`의 "container not found" 메시지는 디스크 문제가 아니라 무해한 잡음으로 분류합니다.

## [0.1.2] - 2026-11-19

### 추가
- `why node`(또는 `nodes`)가 모든 노드를 점검해 문제 있는 노드만 보여주고 정상 노드는 한 줄로 요약합니다. 이전에는 "node"라는 이름의 파드로 해석했습니다.
- `why`의 탭 완성이 `node/`, `nodes`, 노드 이름을 제안하고, `why`·`sh`·`logs`는 `job/`과 job 이름도 제안합니다.

### 수정
- `completion install --alias kk`가 새 bash·fish 셸에서 동작하지 않던 문제를 고쳤습니다. 두 셸은 입력한 이름으로 자동완성 파일을 찾기 때문에, 이제 별칭 이름의 파일(bash: `.../completions/kk`, fish: `kk.fish`)도 함께 씁니다.
- `completion install --help`와 README에 셸별로 어떤 파일이 써지는지 적었습니다.

## [0.1.1] - 2026-11-19

### 추가
- `kubekaze update`: 온라인(`--check`, `--version`), 사내 미러(`--url`, `KUBEKAZE_UPDATE_URL`), 네트워크 없이 받아 둔 아카이브(`--from`)로 바이너리를 업데이트합니다. 체크섬 검증, 교체 전 실행 확인, 원자적 교체를 하고 Homebrew/krew 설치와 평문 http는 거부합니다. 자동 업데이트 확인은 하지 않습니다.
- `why node/<이름>`: 노드 상태 조건, heartbeat, taint, 이벤트, 노드 위 비정상 파드를 보여주고 원인(kubelet 중단, 디스크·메모리·PID 압박, CNI·런타임 문제, cordon)에 순위를 매깁니다.
- `completion --alias <이름>`, `completion install --alias <이름>`: `kk` 같은 셸 alias에서도 자동완성이 동작합니다.
- `ctx`가 전환 직후 API 서버와 로그인을 확인해 만료된 로그인이나 접속 불가를 바로 경고합니다(`--no-check`로 생략).
- `ctx` 선택 화면이 클러스터와 사용자가 같은 컨텍스트(`oc project`가 네임스페이스마다 만드는 것)를 묶어 보여줍니다. `--all`로 전체를 봅니다.
- 모든 명령의 상세한 `--help`: 점검 항목, 대상 지정 방법, 안전 규칙, 다양한 예시, 종료 코드와 환경 변수.

### 변경
- `status`가 `-A` 없이도 노드를 항상 점검하고, heartbeat가 끊기면 `Unknown`으로 표시하며, NotReady의 kubelet 사유를 보여줍니다.

## [0.1.0] - 2026-11-19

첫 릴리즈입니다.

### 추가
- client-go와 사용자의 kubeconfig 기반 CLI: `status`, `why`, `events`, `sh`, `logs`, `rm`, `ctx`, `ns` 명령.
- `status`: 비정상인 것만 표시하며 문제가 있으면 종료 코드 3.
- `why`: 타임라인, owner 체인, 최근 롤아웃, 규칙 기반으로 순위를 매긴 의심 원인(OOMKilled, 이미지 풀, 스케줄링, probe, 볼륨 등).
- `events`: 네임스페이스를 가로지르는 시간순 이벤트, 반복 합치기, 필터, 네임스페이스별 요약.
- `sh`: 셸 탐색(bash/sh/ash/busybox), 워크로드 대상, 단발 명령, 다이제스트로 고정된 권한 없는 임시 디버그 컨테이너.
- `logs`: 멀티 파드 tail, 이전 실행 로그 자동 포함, 에러 강조, 시크릿 마스킹.
- `rm`: 미리보기, UID 조건, 확인, 운영 가드, `--max` 상한이 있는 일괄 파드 삭제.
- `ctx` / `ns`: 이전 값(`-`)을 지원하는 컨텍스트·네임스페이스 전환, OpenShift 프로젝트.
- bash, zsh, fish, PowerShell 셸 자동완성(`kubekaze completion`, `kubekaze completion install`): 컨텍스트, 네임스페이스, 파드, 워크로드, 플래그 값을 실시간으로 완성.
- 그룹으로 나뉜 `--help`와 모든 명령의 예시, 명령·플래그 오타에 대한 "Did you mean" 힌트, 잘못된 명령줄에 대한 종료 코드 2와 사용법 안내.
- `kubekaze version` 명령.
- 클러스터가 제공한 문자열의 터미널 이스케이프 시퀀스 제거.
- 빌드·릴리즈 설정: GoReleaser(linux/macOS/windows x amd64/arm64), CI 매트릭스, govulncheck, Dependabot, 커밋 SHA로 고정한 GitHub Actions.
- 프로젝트 컨셉·설계 문서, 영어/한글 README, 보안 정책, Apache-2.0 라이선스와 NOTICE.
