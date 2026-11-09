"""Serves FOLDER as a stand-in for a GitHub releases page, for
scripts/test-install.sh.

Like github.com/<owner>/<repo>/releases, every <prefix>/latest path redirects
to <prefix>/tag/TAG and every <prefix>/tag/<name> page exists. Any other path
is a file of FOLDER, e.g. <prefix>/download/<tag>/<asset>.

Usage: python3 scripts/test-release-server.py FOLDER TAG PORT
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
        """Answers the latest-release redirect and the tag pages; returns
        False for any other path."""
        if self.path.endswith("/latest"):
            prefix = self.path[: -len("latest")]
            self.send_response(302)
            self.send_header("Location", f"http://{self.headers['Host']}{prefix}tag/{self.latest_tag}")
            self.send_header("Content-Length", "0")
            self.end_headers()
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


def main():
    if len(sys.argv) != 4:
        sys.exit(__doc__)
    folder, tag, port = sys.argv[1], sys.argv[2], int(sys.argv[3])
    ReleasesHandler.latest_tag = tag
    handler = functools.partial(ReleasesHandler, directory=folder)
    http.server.ThreadingHTTPServer(("", port), handler).serve_forever()


if __name__ == "__main__":
    main()
