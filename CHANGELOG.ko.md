# 변경 이력

[English](CHANGELOG.md) | 한국어

주요 변경 사항을 여기에 기록합니다. 형식은 [Keep a Changelog](https://keepachangelog.com/ko/), 버전은 [SemVer](https://semver.org/lang/ko/)를 따릅니다.

## [Unreleased]

### 추가
- client-go와 사용자의 kubeconfig 기반 CLI: `status`, `why`, `events`, `sh`, `logs`, `rm`, `ctx`, `ns` 명령.
- `status`: 비정상인 것만 표시하며 문제가 있으면 종료 코드 3.
- `why`: 타임라인, owner 체인, 최근 롤아웃, 규칙 기반으로 순위를 매긴 의심 원인(OOMKilled, 이미지 풀, 스케줄링, probe, 볼륨 등).
- `events`: 네임스페이스를 가로지르는 시간순 이벤트, 반복 합치기, 필터, 네임스페이스별 요약.
- `sh`: 셸 탐색(bash/sh/ash/busybox), 워크로드 대상, 단발 명령, 다이제스트로 고정된 권한 없는 임시 디버그 컨테이너.
- `logs`: 멀티 파드 tail, 이전 실행 로그 자동 포함, 에러 강조, 시크릿 마스킹.
- `rm`: 미리보기, UID 조건, 확인, 운영 가드, `--max` 상한이 있는 일괄 파드 삭제.
- `ctx` / `ns`: 이전 값(`-`)을 지원하는 컨텍스트·네임스페이스 전환, OpenShift 프로젝트.
- 클러스터가 제공한 문자열의 터미널 이스케이프 시퀀스 제거.
- 빌드·릴리즈 설정: GoReleaser(linux/macOS/windows x amd64/arm64), CI 매트릭스, govulncheck, Dependabot, 커밋 SHA로 고정한 GitHub Actions.
- 프로젝트 컨셉·설계 문서, 영어/한글 README, 보안 정책, Apache-2.0 라이선스와 NOTICE.

게시된 릴리즈는 아직 없습니다.
