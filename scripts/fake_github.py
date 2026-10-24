#!/usr/bin/env python3
"""A GitHub that serves one release of Kitties from a folder, for scripts/selfupdate_test.sh.

  fake_github.py PORT FOLDER TAG

GET /repos/madkoding/kitties/releases/latest answers with TAG and, as its assets, the files of
FOLDER (served at /dl/<name>)."""
import http.server
import json
import os
import sys

port, folder, tag = int(sys.argv[1]), sys.argv[2], sys.argv[3]


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        base = "http://127.0.0.1:%d" % port
        if self.path == "/repos/madkoding/kitties/releases/latest":
            assets = [{"name": n, "browser_download_url": base + "/dl/" + n} for n in sorted(os.listdir(folder))]
            body, ctype = json.dumps({"tag_name": tag, "prerelease": False, "assets": assets}).encode(), "application/json"
        elif self.path.startswith("/dl/") and os.path.isfile(os.path.join(folder, self.path[4:])):
            with open(os.path.join(folder, self.path[4:]), "rb") as f:
                body, ctype = f.read(), "application/octet-stream"
        else:
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


http.server.HTTPServer(("127.0.0.1", port), Handler).serve_forever()
