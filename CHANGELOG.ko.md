# 변경 이력

[English](CHANGELOG.md) | 한국어

주요 변경 사항을 여기에 기록합니다. 형식은 [Keep a Changelog](https://keepachangelog.com/ko/), 버전은 [SemVer](https://semver.org/lang/ko/)를 따릅니다.

## [Unreleased]

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
