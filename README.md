# auth-proxy-poc

인증 정보를 자동으로 주입하는 포워드 프록시 PoC.

## 아키텍처

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
                         │                  │ Python 자동화 │    │  ← 별도 구현
                         │                  │ (세션 유지)    │    │
                         │                  └──────────────┘    │
                         └──────────────────────────────────────┘
```

### 구성 요소

| 구성 요소 | 역할 | 기술 스택 |
|-----------|------|-----------|
| **Proxy** | 포워드 프록시, 인증키 기반 credential 주입 | Go + elazarl/goproxy |
| **API** | 세션 CRUD, 프록시 키 발급 | Go net/http |
| **DB** | 세션/인증정보 저장 | PostgreSQL |
| **자동화** | 로그인 세션 유지 → DB 저장 (별도) | Python |

## 빠른 시작

```bash
# 1. PostgreSQL 실행
docker compose up -d

# 2. 서버 빌드 & 실행
go run ./cmd/server

# 3. 세션 생성 (API)
curl -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test",
    "target_host": "httpbin.org",
    "auth_type": "bearer",
    "credentials": {"token": "my-jwt-token"}
  }'
# → proxy_key 가 반환됨

# 4. 프록시를 통한 요청 (반환된 proxy_key 사용)
curl -x http://localhost:8888 \
  -H "X-Auth-Proxy-Key: apk_..." \
  http://httpbin.org/headers
# → Authorization: Bearer my-jwt-token 이 자동 주입됨
```

## API 엔드포인트

| Method | Path | 설명 |
|--------|------|------|
| POST | `/api/sessions` | 새 세션 생성 + 프록시 키 발급 |
| GET | `/api/sessions` | 전체 세션 목록 |
| GET | `/api/sessions/{id}` | 세션 상세 조회 |
| POST | `/api/sessions/{id}/credentials` | 인증정보 업데이트 |
| POST | `/api/sessions/{id}/revoke` | 세션 무효화 |
| DELETE | `/api/sessions/{id}` | 세션 삭제 |

## 프록시 동작

1. 클라이언트가 `X-Auth-Proxy-Key` 헤더와 함께 프록시로 요청
2. 프록시가 해당 키로 DB에서 세션 조회
3. 세션의 `auth_type`에 따라 인증정보 주입:
   - `cookie`: Cookie 헤더에 병합
   - `bearer`: Authorization: Bearer 헤더 설정
   - `custom_header`: 지정된 커스텀 헤더 설정
4. `X-Auth-Proxy-Key` 헤더 제거 후 타겟으로 포워딩
5. 키가 없거나, 만료/무효이면 오류 응답 (407/403)

## 환경 변수

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `DATABASE_URL` | `postgres://authproxy:authproxy@localhost:5432/authproxy?sslmode=disable` | PostgreSQL 접속 URL |
| `API_ADDR` | `:8080` | API 서버 바인드 주소 |
| `PROXY_ADDR` | `:8888` | 프록시 서버 바인드 주소 |

## 시드 데이터

`docker compose up` 시 `migrations/001_init.sql`이 자동 실행되어 테스트용 세션 2개가 생성됩니다:

- Bearer 세션: 키 `apk_poc_test_key_00000000000000000000000000000001`
- Cookie 세션: 키 `apk_poc_test_key_00000000000000000000000000000002`
