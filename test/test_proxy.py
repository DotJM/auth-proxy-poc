"""
프록시 테스트 스크립트.
직접 요청 vs 프록시 경유 요청의 차이를 비교합니다.

사전 조건:
  1. PostgreSQL 실행 중:  docker compose up -d
  2. Auth Proxy 실행 중:  go run ./cmd/server
  3. Echo 서버 실행 중:   python test/echo_server.py 9000

사용법:
    python test/test_proxy.py

환경 변수:
    ECHO_URL    - 에코 서버 주소 (기본: http://localhost:9000)
    PROXY_URL   - 프록시 주소 (기본: http://localhost:8888)
    API_URL     - API 주소 (기본: http://localhost:8080)
"""

import json
import os
import sys
import urllib.request

ECHO_URL = os.getenv("ECHO_URL", "http://localhost:9000")
PROXY_URL = os.getenv("PROXY_URL", "http://localhost:8888")
API_URL = os.getenv("API_URL", "http://localhost:8080")

SEPARATOR = "=" * 60


def pretty(data):
    return json.dumps(data, indent=2, ensure_ascii=False)


def request_direct(url, headers=None):
    """프록시 없이 직접 요청"""
    req = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read())


def request_via_proxy(url, proxy_key, extra_headers=None):
    """프록시를 경유하여 요청"""
    proxy_handler = urllib.request.ProxyHandler({
        "http": PROXY_URL,
        "https": PROXY_URL,
    })
    opener = urllib.request.build_opener(proxy_handler)

    headers = {"X-Auth-Proxy-Key": proxy_key}
    if extra_headers:
        headers.update(extra_headers)

    req = urllib.request.Request(url, headers=headers)
    with opener.open(req) as resp:
        return json.loads(resp.read())


def request_via_proxy_raw(url, proxy_key):
    """프록시 요청 (에러 포함 처리)"""
    proxy_handler = urllib.request.ProxyHandler({
        "http": PROXY_URL,
        "https": PROXY_URL,
    })
    opener = urllib.request.build_opener(proxy_handler)

    headers = {"X-Auth-Proxy-Key": proxy_key} if proxy_key else {}
    req = urllib.request.Request(url, headers=headers)
    try:
        with opener.open(req) as resp:
            return resp.status, resp.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()


def create_test_session(name, target_host, auth_type, credentials):
    """API를 통해 테스트 세션 생성"""
    payload = json.dumps({
        "name": name,
        "target_host": target_host,
        "auth_type": auth_type,
        "credentials": credentials,
    }).encode()
    req = urllib.request.Request(
        f"{API_URL}/api/sessions",
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read())


def test_1_direct_vs_proxy():
    """테스트 1: 직접 요청 vs 프록시(Bearer) 경유 요청 비교"""
    print(SEPARATOR)
    print("테스트 1: 직접 요청 vs Bearer 프록시 요청")
    print(SEPARATOR)

    # Bearer 세션 생성
    result = create_test_session(
        name="test-bearer",
        target_host="localhost",
        auth_type="bearer",
        credentials={"token": "super-secret-jwt-token-12345"},
    )
    proxy_key = result["proxy_key"]
    session_id = result["session"]["id"]
    print(f"세션 생성: id={session_id}, key={proxy_key[:20]}...")

    # 직접 요청
    print("\n[직접 요청]")
    direct = request_direct(f"{ECHO_URL}/api/data", {"Accept": "application/json"})
    print(f"  Headers received by server:")
    for k, v in direct["headers"].items():
        print(f"    {k}: {v}")

    # 프록시 경유 요청
    print("\n[프록시 경유 요청]")
    proxied = request_via_proxy(f"{ECHO_URL}/api/data", proxy_key, {"Accept": "application/json"})
    print(f"  Headers received by server:")
    for k, v in proxied["headers"].items():
        print(f"    {k}: {v}")

    # 비교
    print("\n[비교]")
    has_auth = "Authorization" in proxied["headers"]
    no_proxy_key = "X-Auth-Proxy-Key" not in proxied["headers"]
    print(f"  Authorization 헤더 주입됨: {'✓' if has_auth else '✗'}")
    print(f"  X-Auth-Proxy-Key 제거됨:   {'✓' if no_proxy_key else '✗'}")
    if has_auth:
        print(f"  Authorization 값: {proxied['headers']['Authorization']}")
    return has_auth and no_proxy_key


def test_2_cookie_injection():
    """테스트 2: Cookie 주입 테스트"""
    print(f"\n{SEPARATOR}")
    print("테스트 2: Cookie 주입")
    print(SEPARATOR)

    result = create_test_session(
        name="test-cookie",
        target_host="localhost",
        auth_type="cookie",
        credentials={"session_id": "abc123", "csrf_token": "xyz789"},
    )
    proxy_key = result["proxy_key"]
    print(f"세션 생성: key={proxy_key[:20]}...")

    proxied = request_via_proxy(f"{ECHO_URL}/test", proxy_key)
    cookie_header = proxied["headers"].get("Cookie", "")
    print(f"  Cookie 헤더: {cookie_header}")

    has_session = "session_id=abc123" in cookie_header
    has_csrf = "csrf_token=xyz789" in cookie_header
    print(f"  session_id 주입됨: {'✓' if has_session else '✗'}")
    print(f"  csrf_token 주입됨: {'✓' if has_csrf else '✗'}")
    return has_session and has_csrf


def test_3_custom_header():
    """테스트 3: Custom Header 주입 테스트"""
    print(f"\n{SEPARATOR}")
    print("테스트 3: Custom Header 주입")
    print(SEPARATOR)

    result = create_test_session(
        name="test-custom",
        target_host="localhost",
        auth_type="custom_header",
        credentials={"X-API-Key": "key-abc-123", "X-Tenant-ID": "tenant-42"},
    )
    proxy_key = result["proxy_key"]
    print(f"세션 생성: key={proxy_key[:20]}...")

    proxied = request_via_proxy(f"{ECHO_URL}/test", proxy_key)
    headers = proxied["headers"]
    print(f"  수신된 헤더:")
    for k, v in headers.items():
        if k.startswith("X-"):
            print(f"    {k}: {v}")

    has_api_key = headers.get("X-Api-Key") == "key-abc-123" or headers.get("X-API-Key") == "key-abc-123"
    has_tenant = headers.get("X-Tenant-Id") == "tenant-42" or headers.get("X-Tenant-ID") == "tenant-42"
    print(f"  X-API-Key 주입됨:   {'✓' if has_api_key else '✗'}")
    print(f"  X-Tenant-ID 주입됨: {'✓' if has_tenant else '✗'}")
    return has_api_key and has_tenant


def test_4_error_cases():
    """테스트 4: 오류 케이스 (키 없음, 잘못된 키)"""
    print(f"\n{SEPARATOR}")
    print("테스트 4: 오류 처리")
    print(SEPARATOR)

    # 키 없이 요청
    print("\n[키 없이 요청]")
    status, body = request_via_proxy_raw(f"{ECHO_URL}/test", proxy_key=None)
    print(f"  상태: {status}")
    print(f"  응답: {body.strip()}")
    no_key_rejected = status == 407

    # 잘못된 키로 요청
    print("\n[잘못된 키로 요청]")
    status, body = request_via_proxy_raw(f"{ECHO_URL}/test", proxy_key="apk_invalid_key")
    print(f"  상태: {status}")
    print(f"  응답: {body.strip()}")
    bad_key_rejected = status == 403

    print(f"\n  키 없음 → 거부됨: {'✓' if no_key_rejected else '✗'} (status={status})")
    print(f"  잘못된 키 → 거부됨: {'✓' if bad_key_rejected else '✗'}")
    return no_key_rejected and bad_key_rejected


def test_5_revoke_session():
    """테스트 5: 세션 무효화 후 프록시 거부 확인"""
    print(f"\n{SEPARATOR}")
    print("테스트 5: 세션 무효화(revoke) 후 거부")
    print(SEPARATOR)

    result = create_test_session(
        name="test-revoke",
        target_host="localhost",
        auth_type="bearer",
        credentials={"token": "will-be-revoked"},
    )
    proxy_key = result["proxy_key"]
    session_id = result["session"]["id"]

    # 먼저 정상 동작 확인
    status, _ = request_via_proxy_raw(f"{ECHO_URL}/test", proxy_key)
    print(f"  무효화 전: status={status}")
    before_ok = status == 200

    # 세션 무효화
    req = urllib.request.Request(
        f"{API_URL}/api/sessions/{session_id}/revoke",
        data=b"",
        method="POST",
    )
    urllib.request.urlopen(req)
    print(f"  세션 무효화 완료")

    # 무효화 후 요청
    status, body = request_via_proxy_raw(f"{ECHO_URL}/test", proxy_key)
    print(f"  무효화 후: status={status}")
    after_rejected = status == 403

    print(f"\n  무효화 전 통과: {'✓' if before_ok else '✗'}")
    print(f"  무효화 후 거부: {'✓' if after_rejected else '✗'}")
    return before_ok and after_rejected


def main():
    print("Auth Proxy PoC 테스트")
    print(f"  Echo:  {ECHO_URL}")
    print(f"  Proxy: {PROXY_URL}")
    print(f"  API:   {API_URL}")
    print()

    results = {}
    tests = [
        ("Bearer 주입", test_1_direct_vs_proxy),
        ("Cookie 주입", test_2_cookie_injection),
        ("Custom Header 주입", test_3_custom_header),
        ("오류 처리", test_4_error_cases),
        ("세션 무효화", test_5_revoke_session),
    ]

    for name, fn in tests:
        try:
            results[name] = fn()
        except Exception as e:
            print(f"\n  ✗ 오류 발생: {e}")
            results[name] = False

    # 결과 요약
    print(f"\n{SEPARATOR}")
    print("결과 요약")
    print(SEPARATOR)
    all_pass = True
    for name, passed in results.items():
        status = "✓ PASS" if passed else "✗ FAIL"
        print(f"  {status}  {name}")
        if not passed:
            all_pass = False

    print()
    sys.exit(0 if all_pass else 1)


if __name__ == "__main__":
    main()
