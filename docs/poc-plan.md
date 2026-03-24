# Auth Proxy PoC 계획서

## 1. 프로젝트 개요

### 목적
마이크로서비스가 외부 웹사이트에 아웃바운드 요청을 보낼 때, 브라우저 자동화로 획득한 인증정보(쿠키, 토큰, 헤더)를 자동으로 주입하는 중간 프록시 서비스.

### 핵심 가치
- 브라우저 자동화 + 포워드 프록시를 결합한 오픈소스가 현재 존재하지 않음
- 다양한 인증 방식(쿠키, Bearer 토큰, 커스텀 헤더)을 통합 처리

## 2. 아키텍처

```
┌──────────────┐        ┌──────────────────────────────────┐        ┌──────────────┐
│ Microservice │──req──▶│        Auth Proxy Service         │──req──▶│ Target Site  │
│  (Client)    │◀─res───│                                   │◀─res───│              │
└──────────────┘        │  ┌────────────┐  ┌─────────────┐  │        └──────────────┘
                        │  │ mitmproxy  │  │ Credential  │  │
                        │  │  (addon)   │──│   Store     │  │
                        │  └────────────┘  └──────┬──────┘  │
                        │                         │         │
                        │                ┌────────┴───────┐ │
                        │                │   Playwright    │ │
                        │                │ (Login Runner)  │ │
                        │                └────────────────┘ │
                        └──────────────────────────────────┘
```

### 요청 흐름
1. 클라이언트가 `HTTP_PROXY` 설정으로 프록시를 경유하여 타겟 사이트에 요청
2. mitmproxy addon이 요청을 가로채서 타겟 도메인 확인
3. Credential Store에서 해당 도메인+계정의 캐시된 인증정보 조회
4. **캐시 히트** → 인증정보를 요청 헤더/쿠키에 주입 후 포워딩
5. **캐시 미스** → Playwright로 로그인 수행 → 인증정보 추출 → 저장 → 주입 후 포워딩
6. 응답에서 401/403 감지 시 → 인증정보 갱신 트리거

## 3. 기술 스택

| 영역 | 선택 | 근거 |
|------|------|------|
| 언어 | **Python 3.11+** | mitmproxy, Playwright 모두 Python 우선 지원 |
| 프록시 엔진 | **mitmproxy** | 프로그래밍 가능 addon 시스템, HTTP/HTTPS/HTTP2 지원, 활발한 개발 |
| 브라우저 자동화 | **Playwright** | `storage_state()` 로 쿠키+localStorage 일괄 추출, auto-wait 내장, headless 기본 |
| 인증정보 저장 | **인메모리 dict** (PoC) | TTL 기반 캐싱, asyncio.Lock으로 동시성 처리. 프로덕션에서는 Redis |
| API 서버 | **FastAPI** (관리용) | 인증정보 등록/조회/삭제 관리 API, 사이트 설정 관리 |

### 핵심 라이브러리
- `mitmproxy` — 포워드 프록시 + request/response 훅
- `playwright` — 헤드리스 브라우저 로그인 자동화
- `fastapi` + `uvicorn` — 관리 API 서버
- `httpx` — 비동기 HTTP 클라이언트 (내부 통신용)
- `pydantic` — 설정/데이터 모델 검증

## 4. 핵심 컴포넌트 설계

### 4.1 Credential Store

```python
# 키: (domain, account_id) 튜플
# 값: Credential 객체 (type, value, expires_at, metadata)

@dataclass
class Credential:
    type: Literal["cookie", "bearer", "custom_header"]
    value: dict  # cookies dict, token string, or headers dict
    expires_at: datetime
    site_config: SiteConfig

class CredentialStore:
    async def get(domain, account_id) -> Credential | None
    async def set(domain, account_id, credential) -> None
    async def invalidate(domain, account_id) -> None
    async def is_expired(domain, account_id) -> bool
```

### 4.2 mitmproxy Addon (Auth Injector)

```python
class AuthInjectorAddon:
    def request(self, flow: http.HTTPFlow):
        """요청 가로채기 → 인증정보 주입"""
        domain = flow.request.host
        account_id = self._resolve_account(flow)  # 헤더 또는 설정 기반

        cred = self.store.get(domain, account_id)
        if not cred or cred.is_expired():
            cred = self.login_runner.run(domain, account_id)

        self._inject(flow, cred)  # Cookie/Authorization/Custom 헤더 주입

    def response(self, flow: http.HTTPFlow):
        """401/403 감지 → 인증정보 갱신"""
        if flow.response.status_code in (401, 403):
            self.store.invalidate(domain, account_id)
```

### 4.3 Login Runner (Playwright)

```python
class LoginRunner:
    async def login(self, site_config: SiteConfig, account: Account) -> Credential:
        async with async_playwright() as p:
            browser = await p.chromium.launch(headless=True)
            context = await browser.new_context()
            page = await context.new_page()

            # 1. 로그인 페이지 이동
            await page.goto(site_config.login_url)

            # 2. 로그인 수행 (사이트별 로그인 스크립트 실행)
            await site_config.login_script(page, account)

            # 3. 인증정보 추출
            cookies = await context.cookies()
            storage = await page.evaluate('() => ({...localStorage})')

            # 4. Credential 객체 생성 및 반환
            return self._build_credential(site_config, cookies, storage)
```

### 4.4 Site Config (사이트별 설정)

```python
@dataclass
class SiteConfig:
    domain: str
    login_url: str
    auth_type: Literal["cookie", "bearer", "custom_header"]

    # 인증정보 추출 설정
    cookie_names: list[str] | None          # 필요한 쿠키 이름 필터
    token_storage_key: str | None           # localStorage에서 토큰을 읽을 키
    custom_header_mapping: dict[str, str] | None  # 커스텀 헤더 매핑

    # 수명 관리
    credential_ttl_seconds: int = 1800      # 기본 30분

    # 로그인 스크립트 (Python callable 또는 스크립트 경로)
    login_script_path: str                  # 사이트별 로그인 로직
```

### 4.5 관리 API (FastAPI)

```
POST   /api/sites                   # 사이트 설정 등록
GET    /api/sites                   # 사이트 목록 조회
GET    /api/sites/{domain}          # 사이트 설정 조회
DELETE /api/sites/{domain}          # 사이트 설정 삭제

POST   /api/accounts               # 계정 등록
GET    /api/credentials             # 캐시된 인증정보 목록
DELETE /api/credentials/{key}       # 인증정보 수동 무효화
POST   /api/credentials/{key}/refresh  # 인증정보 수동 갱신
```

## 5. 프록시 사용 방식

### 클라이언트 측 (마이크로서비스)
```bash
# 환경변수로 프록시 설정
export HTTP_PROXY=http://auth-proxy:8080
export HTTPS_PROXY=http://auth-proxy:8080

# 이후 일반적인 HTTP 요청만 보내면 됨
curl https://target-site.com/api/data
# → 프록시가 자동으로 인증정보를 주입하여 포워딩
```

### 계정 연결 (커스텀 헤더 방식)
```bash
# 어떤 계정을 사용할지 지정이 필요한 경우
curl -H "X-Auth-Proxy-Account: user123" https://target-site.com/api/data
# → 프록시가 X-Auth-Proxy-Account 헤더를 읽고 해당 계정의 인증정보 사용
# → X-Auth-Proxy-* 헤더는 타겟 서버로 포워딩하기 전에 제거
```

## 6. 프로젝트 구조

```
auth-proxy-poc/
├── src/
│   ├── __init__.py
│   ├── main.py                    # 엔트리포인트 (mitmproxy + FastAPI 실행)
│   ├── config.py                  # 전역 설정
│   │
│   ├── proxy/
│   │   ├── __init__.py
│   │   ├── addon.py               # mitmproxy addon (AuthInjectorAddon)
│   │   └── launcher.py            # mitmproxy 프로세스 관리
│   │
│   ├── auth/
│   │   ├── __init__.py
│   │   ├── login_runner.py        # Playwright 로그인 실행기
│   │   ├── credential.py          # Credential 데이터 모델
│   │   └── extractors.py          # 쿠키/토큰/헤더 추출 유틸
│   │
│   ├── store/
│   │   ├── __init__.py
│   │   ├── base.py                # CredentialStore 인터페이스
│   │   └── memory.py              # 인메모리 구현
│   │
│   ├── api/
│   │   ├── __init__.py
│   │   ├── app.py                 # FastAPI 앱
│   │   └── routes.py              # API 라우트
│   │
│   └── sites/                     # 사이트별 로그인 스크립트
│       ├── __init__.py
│       └── example_site.py        # 예시 로그인 스크립트
│
├── tests/
│   ├── test_addon.py
│   ├── test_credential_store.py
│   ├── test_login_runner.py
│   └── test_integration.py
│
├── docs/
│   └── poc-plan.md
├── requirements.txt
├── pyproject.toml
└── README.md
```

## 7. 구현 단계 (Phase)

### Phase 1: 기본 인프라 (Week 1)
- [ ] 프로젝트 구조 셋업 (pyproject.toml, 의존성)
- [ ] Credential 데이터 모델 정의
- [ ] 인메모리 CredentialStore 구현
- [ ] 기본 SiteConfig 모델 정의

### Phase 2: 프록시 코어 (Week 1-2)
- [ ] mitmproxy addon 구현 (AuthInjectorAddon)
  - 요청 가로채기 → 도메인 매칭 → 인증정보 주입
  - Cookie, Bearer, Custom Header 주입 로직
  - X-Auth-Proxy-* 메타 헤더 처리 및 제거
- [ ] mitmproxy 실행 래퍼 구현
- [ ] 401/403 응답 감지 → 인증정보 무효화 로직

### Phase 3: 브라우저 자동화 (Week 2)
- [ ] LoginRunner 구현 (Playwright 기반)
- [ ] 쿠키 추출기 (context.cookies())
- [ ] localStorage 토큰 추출기 (page.evaluate)
- [ ] 사이트별 로그인 스크립트 인터페이스 정의
- [ ] 예시 로그인 스크립트 작성 (httpbin 또는 테스트 사이트)

### Phase 4: 관리 API (Week 2-3)
- [ ] FastAPI 관리 서버 구현
- [ ] 사이트 설정 CRUD API
- [ ] 인증정보 조회/무효화/갱신 API

### Phase 5: 통합 & 테스트 (Week 3)
- [ ] mitmproxy + LoginRunner + CredentialStore 통합
- [ ] 엔드투엔드 테스트 (테스트 웹서버 + 프록시 + 클라이언트)
- [ ] 동시 요청 처리 테스트
- [ ] 인증정보 만료 → 자동 갱신 테스트

## 8. 핵심 고려사항

### HTTPS 프록시 처리
- mitmproxy가 자동으로 CA 인증서 생성 (`~/.mitmproxy/`)
- 클라이언트에서 해당 CA를 신뢰하도록 설정 필요 (`REQUESTS_CA_BUNDLE` 등)
- 인증서 피닝이 있는 사이트는 프록시 불가

### 동시성 처리
- mitmproxy는 asyncio 기반 → addon에서 `asyncio.Lock` 사용
- 같은 (domain, account)로 동시에 여러 요청이 올 때 로그인을 한 번만 수행
- 로그인 진행 중인 요청은 대기 후 결과 공유 (asyncio.Event)

### 인증정보 수명 관리
- TTL 기반 자동 만료 (기본 30분, 사이트별 설정 가능)
- 401/403 감지 시 즉시 무효화 → 재인증
- JWT 토큰의 경우 `exp` 클레임 파싱하여 정확한 만료 시간 사용

### PoC 범위 제한
- 단일 프로세스, 단일 노드
- 인메모리 저장소 (재시작 시 초기화)
- 수동 사이트 설정 (자동 감지 미구현)
- 기본적인 에러 핸들링만 구현

## 9. 참고 자료

### 프록시
- [mitmproxy 공식 문서](https://docs.mitmproxy.org/stable/)
- [mitmproxy Addon 시스템](https://docs.mitmproxy.org/stable/addons/examples/)
- [mitmproxy 코드 내장 실행](https://github.com/mitmproxy/mitmproxy/discussions/5255)

### 브라우저 자동화
- [Playwright Python Auth 가이드](https://playwright.dev/python/docs/auth)
- [Playwright BrowserContext API](https://playwright.dev/python/docs/api/class-browsercontext)
- [selenium-wire](https://github.com/wkeeling/selenium-wire) — 참고용

### 유사 프로젝트
- [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy) — 리버스 프록시 + OAuth2 (방향은 반대)
- [proxy-login-automator](https://github.com/sjitech/proxy-login-automator) — 프록시 인증 자동화 (407만 처리)
