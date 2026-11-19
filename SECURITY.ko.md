# 보안 정책

[English](SECURITY.md) | 한국어

> 상태: 릴리즈 전입니다. 아래 모델은 첫 릴리즈에서 지킬 약속이며, *(계획)* 표시가 있는 항목은 아직 구현되지 않았습니다.

## 취약점 제보

보안 문제는 **공개 이슈로 올리지 마세요.**

이 저장소의 **Security → Report a vulnerability**(GitHub 비공개 제보)를 이용해 주세요. 버전, 환경(배포판, 쿠버네티스 버전), 재현 절차를 포함해 주세요. 영업일 기준 5일 내 접수 확인을 목표로 하며, 확인된 이슈는 가능한 한 빨리 수정 또는 완화책을 내고 원하시는 분은 제보자로 표기합니다.

## 지원 버전

1.0 이전에는 최신 마이너 릴리즈만, 1.0 이후에는 최신 두 개의 마이너 릴리즈에 보안 수정을 제공합니다.

## 보안 모델

| 영역 | 약속 |
|------|------|
| 자격 증명 | 기존 kubeconfig와 exec 인증 플러그인을 그대로 사용합니다. 자격 증명이나 토큰을 저장·복사·로깅·전송하지 않습니다. |
| 기본 읽기 전용 | `status`, `why`, `logs`, `changes`는 get/list/watch와 로그 읽기만 수행합니다. |
| 변경 동작 | `sh`(pods/exec), 임시 디버그 컨테이너 등은 명시적으로 실행되고 출력에 표시되며, 운영(prod)으로 표시된 컨텍스트에서는 확인을 받습니다. |
| 일괄 삭제 | `rm`은 항상 미리보기를 먼저 보여주고 확인 후에만 삭제합니다. 파드를 UID로 삭제하므로 같은 이름으로 재생성된 파드는 건드리지 않습니다. 10개 이상이면 개수를, 운영(prod)으로 보이는 컨텍스트에서는 컨텍스트 이름을 직접 입력해야 하며(스크립트는 `--yes --confirm-context=<이름>`), `--max`(기본 50)가 `--yes`에서도 한 번의 실행 상한을 적용하고, 오브젝트별 결과를 보고합니다. |
| kubeconfig 쓰기 | `ctx` / `ns`는 `current-context`와 해당 컨텍스트의 `namespace` 필드만 client-go의 표준 쓰기(`kubectl config use-context`와 같은 방식)로 바꿉니다. kubeconfig의 다른 부분은 건드리지 않으며, "이전" 값은 사용자 전용 캐시 파일(권한 0600)에 기억합니다. |
| Secret | Secret `data`는 출력하지 않습니다. Secret에서 온 env 값은 마스킹합니다. ConfigMap/Secret 변경은 해시로만 보고합니다. |
| 로그 마스킹 | 흔한 토큰·비밀번호 패턴은 기본적으로 마스킹하며, `--no-redact`를 명시해야 해제됩니다. |
| 텔레메트리 | 없음. 대상 쿠버네티스 API 서버 외에는 네트워크 호출을 하지 않습니다(선택 기능인 AI를 켠 경우에만 해당 LLM 엔드포인트 호출). |
| AI 기능 *(계획)* | 기본 꺼짐. 마스킹되고 사용자가 `--dry-run`으로 미리 볼 수 있는 데이터만 사용자가 지정한 엔드포인트로 보냅니다. 로컬 모델을 지원합니다. |
| 디버그 이미지 | 임시 디버그 컨테이너는 권한 없이 동작합니다(권한 상승 금지, 모든 capability 제거, 기본 seccomp). 이미지는 설정 가능하며 기본은 다이제스트로 고정하고, `latest`나 태그 없는 이미지는 거부합니다. |
| 최소 권한 | 명령별 필요 RBAC를 문서화하고, 권한이 없으면 우회하지 않고 알립니다. |
| 터미널 안전 | 클러스터가 제공하는 문자열(이벤트, 로그, 이름)의 제어 문자를 이스케이프하여 터미널 이스케이프 시퀀스 주입을 막습니다. |
| 멀티 클러스터 *(계획)* | Fan-out은 컨텍스트별로 해당 컨텍스트의 자격 증명으로 실행하며, 자격 증명을 섞지 않습니다. |

## 공급망

- 릴리즈는 태그된 커밋에서 GitHub Actions로 빌드하며, 액션은 커밋 SHA로 고정합니다.
- 산출물에 SHA-256 체크섬, 서명된 체크섬 파일(cosign keyless), SBOM(SPDX), SLSA 빌드 출처 증명을 함께 제공합니다.
- 의존성은 Dependabot으로 추적하고 `govulncheck`로 점검합니다.
- 재현 가능한 빌드: `-trimpath`, 정적 바이너리(`CGO_ENABLED=0`), ldflags로 버전 정보 주입.

## 릴리즈 검증 방법

각 릴리즈는 `checksums.txt`와 keyless cosign 번들 `checksums.txt.sigstore.json`을 게시합니다. 사용하는 플랫폼의 압축 파일과 함께 받은 뒤 아래를 실행하세요(예시는 v0.1.0).

```bash
# 1. 압축 파일이 체크섬 목록과 일치하는지
shasum -a 256 -c checksums.txt --ignore-missing      # Linux: sha256sum -c checksums.txt --ignore-missing

# 2. 체크섬 목록이 이 저장소의 릴리즈 워크플로로 서명됐는지
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/beomzh/kubekaze/\.github/workflows/release\.yml@refs/tags/v0\.1\.0$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

모든 압축 파일에는 SPDX SBOM(`*.sbom.json`)도 함께 제공됩니다.
