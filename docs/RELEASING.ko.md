# 릴리즈와 패키징

[English](RELEASING.md) | 한국어

## 버전 정책

[유의적 버전](https://semver.org/lang/ko/)을 따릅니다. 태그는 `vMAJOR.MINOR.PATCH` 형식입니다.

- `0.x`: 1.0 이전이며 마이너 릴리즈에서 호환성이 깨질 수 있습니다(CHANGELOG에 명시).
- 변경이 병합될 때마다 패치 버전을 올리고, 새 마이너마다 태그와 GitHub Release를 발행합니다.
- 사전 릴리즈는 `-rc.N`을 씁니다(예: `v0.2.0-rc.1`).

## 릴리즈 절차

1. CI 통과 상태로 `main`에 병합합니다.
2. `CHANGELOG.md`를 갱신합니다([Keep a Changelog](https://keepachangelog.com/ko/) 형식).
3. 주석 태그를 푸시합니다(서명 키가 있으면 `-s` 사용): `git tag -a v0.2.0 -m "v0.2.0" && git push origin v0.2.0`. 릴리즈 산출물은 워크플로가 직접 서명합니다(cosign, keyless).
4. GitHub Actions가 GoReleaser를 실행해 linux/darwin/windows × amd64/arm64를 빌드하고(Linux/macOS는 `.tar.gz`, Windows는 `.zip`), 아카이브·체크섬·SBOM·cosign 서명·출처 증명·GitHub Release를 만듭니다.

## 배포 채널

| 채널 | 비고 |
|------|------|
| GitHub Releases | 바이너리의 기준 출처 |
| Homebrew tap | `brew install <owner>/tap/<name>` (GoReleaser가 생성) |
| krew | `kubectl <name>` 플러그인 매니페스트, 바이너리 이름은 `kubectl-<name>` |
| 컨테이너 이미지 | distroless, nonroot, GHCR 게시, 서명 |
| `go install` | 개발자용 지원 |

## GitHub 저장소 설정 (생성 시점)

- **Description:** `Terminal-first Kubernetes companion: find out what is wrong and why in one command. Smart exec, crash-aware logs, multi-cluster triage.`
- **Topics:** `kubernetes`, `kubectl`, `kubectl-plugin`, `cli`, `troubleshooting`, `diagnostics`, `devops`, `sre`, `multi-cluster`, `openshift`, `eks`, `aks`, `gke`, `rancher`, `golang`
- **License:** Apache-2.0
- **보안:** 비공개 취약점 제보, push protection이 있는 secret scanning, Dependabot 알림·업데이트를 켭니다.
- **`main` 브랜치 보호:** PR 필수, CI 통과 필수, 선형 이력, force push 금지.
- **Actions:** 서드파티 액션은 커밋 SHA로 고정하고, 기본 `GITHUB_TOKEN` 권한은 읽기 전용으로 둡니다.
- **기본 브랜치:** `main`.
