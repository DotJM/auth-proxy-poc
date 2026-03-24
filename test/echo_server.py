"""
테스트용 에코 서버.
받은 요청의 모든 정보(메서드, 경로, 헤더, 바디)를 JSON으로 응답합니다.

사용법:
    python test/echo_server.py [포트]
    기본 포트: 9000
"""

import json
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler


class EchoHandler(BaseHTTPRequestHandler):
    def _handle(self):
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length).decode("utf-8") if content_length > 0 else ""

        response = {
            "method": self.command,
            "path": self.path,
            "headers": dict(self.headers),
            "body": body,
        }

        payload = json.dumps(response, indent=2, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    do_GET = _handle
    do_POST = _handle
    do_PUT = _handle
    do_DELETE = _handle
    do_PATCH = _handle

    def log_message(self, format, *args):
        print(f"  [echo] {args[0]}")


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9000
    server = HTTPServer(("0.0.0.0", port), EchoHandler)
    print(f"Echo server listening on http://localhost:{port}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nshutdown")


if __name__ == "__main__":
    main()
