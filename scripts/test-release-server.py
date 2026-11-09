"""Serves FOLDER as a stand-in for a GitHub releases page, for
scripts/test-install.sh and scripts/test-install.ps1.

Like github.com/<owner>/<repo>/releases:
- <prefix>/latest redirects to <prefix>/tag/TAG, and every
  <prefix>/tag/<name> page exists;
- <prefix>/latest/download/<file> redirects to <prefix>/download/TAG/<file>.
Any other path is a file of FOLDER, e.g. <prefix>/download/<tag>/<file>.

Usage: python3 scripts/test-release-server.py FOLDER TAG PORT [ADDRESS]
ADDRESS defaults to all interfaces.
"""

import functools
import http.server
import sys


class ReleasesHandler(http.server.SimpleHTTPRequestHandler):
    latest_tag = ""

    def do_GET(self):
        if not self.answer_page(send_body=True):
            super().do_GET()

    def do_HEAD(self):
        if not self.answer_page(send_body=False):
            super().do_HEAD()

    def answer_page(self, send_body):
        """Answers the latest-release redirects and the tag pages; returns
        False for any other path."""
        if "/latest/download/" in self.path:
            prefix, file = self.path.split("/latest/download/", 1)
            self.redirect(f"{prefix}/download/{self.latest_tag}/{file}")
            return True
        if self.path.endswith("/latest"):
            self.redirect(f"{self.path[: -len('/latest')]}/tag/{self.latest_tag}")
            return True
        if "/tag/" in self.path:
            body = b"release page\n"
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if send_body:
                self.wfile.write(body)
            return True
        return False

    def redirect(self, path):
        self.send_response(302)
        self.send_header("Location", f"http://{self.headers['Host']}{path}")
        self.send_header("Content-Length", "0")
        self.end_headers()


def main():
    if len(sys.argv) not in (4, 5):
        sys.exit(__doc__)
    folder, tag, port = sys.argv[1], sys.argv[2], int(sys.argv[3])
    address = sys.argv[4] if len(sys.argv) == 5 else ""
    ReleasesHandler.latest_tag = tag
    handler = functools.partial(ReleasesHandler, directory=folder)
    http.server.ThreadingHTTPServer((address, port), handler).serve_forever()


if __name__ == "__main__":
    main()
