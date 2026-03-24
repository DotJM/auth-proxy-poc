# Auth Proxy PoC 계획서

## 1. 프로젝트 개요

### 목적
마이크로서비스가 외부 웹사이트에 아웃바운드 요청을 보낼 때, 별도로 유지되는 인증 세션의 정보(쿠키, 토큰, 헤더)를 자동으로 주입하는 포워드 프록시 서비스.

### 핵심 가치
- 클라이언트는 인증 로직을 몰라도 됨 — 프록시 키만 있으면 인증된 요청 가능
- 인증 세션 관리와 실제 요청을 분리하여 관심사 분리 달성
- 다양한 인증 방식(쿠키, Bearer 토큰, 커스텀 헤더)을 통합 처리

## 2. 아키텍처

```
┌──────────────┐         ┌──────────────────────────────────────┐         ┌──────────────┐
│  Client      │──req──▶ │         Auth Proxy (Go)              │──req──▶ │ Target Site  │
│              │◀─res──  │                                      │◀─res──  │              │
│ X-Auth-      │         │  ┌──────────┐     ┌──────────────┐   │         └──────────────┘
│ Proxy-Key    │         │  │ goproxy  │────▶│  PostgreSQL  │   │
│ 헤더 포함     │         │  │ (forward)│     │  (sessions)  │   │
└──────────────┘         │  └──────────┘     └──────┬───────┘   │
                         │                          │           │
                         │  ┌──────────┐            │           │
                         │  │ HTTP API │────────────┘           │
                         │  │ (관리)   │                        │
                         │  └──────────┘                        │
                         │                  ┌──────────────┐    │
                         │                  │ Python 자동화 │    │  ← 별도 프로세스
                         │                  │ (세션 유지)    │    │
                         │                  └──────────────┘    │
                         └──────────────────────────────────────┘
```

### 3개 컴포넌트

1. **API부 (Go)** — HTTP API로 세션 생성/조회/삭제, 프록시 키 발급
2. **프록시부 (Go + goproxy)** — 포워드 프록시로 동작하며, 프록시 키를 기반으로 DB에서 인증 정보를 조회하여 요청에 주입
3. **자동화 로그인 세션 유지부 (Python)** — 별도 프로세스로, 인자값에 따라 로그인 세션을 유지하고 DB에 인증 정보 저장 (별도 구현)

### 요청 흐름
1. 클라이언트가 `X-Auth-Proxy-Key` 헤더를 포함하여 프록시 경유 요청
2. 프록시가 키로 DB에서 활성 세션 조회
3. 세션의 인증 정보(쿠키/토큰/헤더)를 요청에 주입
4. `X-Auth-Proxy-Key` 헤더 제거 후 타겟으로 포워딩
5. 키가 없거나 만료/무효인 경우 즉시 오류 응답

## 3. 기술 스택

| 영역 | 선택 | 근거 |
|------|------|------|
| 프록시 엔진 | **Go + elazarl/goproxy** | 고성능, 프로그래밍 가능한 포워드 프록시, HTTP/HTTPS 지원 |
| API 서버 | **Go net/http** | 프록시와 동일 바이너리, Go 1.22+ 라우팅 패턴 활용 |
| DB | **PostgreSQL 16** | 세션/인증정보 저장, JSONB로 유연한 credential 저장 |
| 자동화 | **Python** (별도) | 로그인 세션 유지, DB 직접 접근 |

## 4. 핵심 컴포넌트 설계

### 4.1 데이터 모델

```sql
-- sessions: 인증 세션
CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    target_host TEXT NOT NULL,          -- 프록시가 허용하는 대상 호스트
    auth_type   TEXT NOT NULL,          -- cookie | bearer | custom_header
    credentials JSONB NOT NULL,         -- 인증 정보 (key-value)
    status      TEXT NOT NULL,          -- active | expired | revoked
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- proxy_keys: 프록시 인증 키 → 세션 매핑
CREATE TABLE proxy_keys (
    key         TEXT PRIMARY KEY,       -- apk_xxxx 형식
    session_id  UUID REFERENCES sessions(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 4.2 인증 정보 주입 방식

| auth_type | credentials 예시 | 주입 방식 |
|-----------|-----------------|-----------|
| `cookie` | `{"session_id": "abc", "csrf": "xyz"}` | Cookie 헤더에 병합 |
| `bearer` | `{"token": "jwt..."}` | `Authorization: Bearer {token}` |
| `custom_header` | `{"X-API-Key": "key123"}` | 해당 헤더를 그대로 설정 |

### 4.3 프록시 보안

- `X-Auth-Proxy-Key` 헤더는 타겟으로 **절대 포워딩하지 않음**
- 세션의 `target_host`와 요청 대상 호스트가 일치해야만 포워딩
- 와일드카드 호스트 지원: `*.example.com`
- 만료된 세션은 자동으로 거부

## 5. API 엔드포인트

```
POST   /api/sessions                  # 세션 생성 + 프록시 키 발급
GET    /api/sessions                  # 세션 목록
GET    /api/sessions/{id}             # 세션 상세
POST   /api/sessions/{id}/credentials # 인증정보 업데이트
POST   /api/sessions/{id}/revoke      # 세션 무효화
DELETE /api/sessions/{id}             # 세션 삭제
```

## 6. 프로젝트 구조

```
auth-proxy-poc/
├── cmd/server/main.go            # 엔트리포인트 (API + Proxy 동시 실행)
├── internal/
│   ├── api/handler.go            # HTTP API 핸들러
│   ├── proxy/proxy.go            # goproxy 기반 포워드 프록시
│   ├── store/postgres.go         # PostgreSQL 접근 계층
│   └── model/model.go            # 데이터 모델
├── migrations/001_init.sql       # 스키마 + 시드 데이터
├── scripts/seed.sh               # API를 통한 테스트 데이터 생성
├── docker-compose.yml            # PostgreSQL
├── go.mod
└── README.md
```

## 7. Python 자동화 연동 (별도 구현)

Python 자동화 스크립트는 다음과 같이 DB에 직접 접근하여 인증 정보를 업데이트:

```python
# 예시: 세션의 credential을 업데이트
import psycopg2, json

conn = psycopg2.connect("dbname=authproxy user=authproxy password=authproxy")
cur = conn.cursor()
cur.execute(
    "UPDATE sessions SET credentials = %s, updated_at = NOW() WHERE id = %s",
    (json.dumps({"token": "new-refreshed-token"}), session_id)
)
conn.commit()
```

또는 API를 통해 업데이트:

```bash
curl -X POST http://localhost:8080/api/sessions/{id}/credentials \
  -H "Content-Type: application/json" \
  -d '{"credentials": {"token": "new-refreshed-token"}}'
```
