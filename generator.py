#!/usr/bin/env python3

import argparse
import http.client
import random
import socket
import sys
import threading
import time
import urllib.error
import urllib.request
from urllib.parse import urlencode, urlparse


def prepare_url(base_url):
    u = urlparse(base_url)
    scheme = (u.scheme or "http").lower()
    if scheme != "http":
        raise ValueError(f"unsupported scheme {scheme!r}: only http is supported")
    host = u.hostname
    if not host:
        raise ValueError("empty host in URL")
    port = u.port or 80
    path = u.path or "/"
    infos = socket.getaddrinfo(host, None, family=socket.AF_INET)
    ip = infos[0][4][0]
    return ip, port, path


def orig_worker(worker_id, host, port, path, stop_event, interval, timeout, stats):
    base_url = f"http://{host}:{port}{path}"
    while not stop_event.is_set():
        num = random.randint(-100, 100)
        url = f"{base_url}?{urlencode({'num': num})}"
        req = urllib.request.Request(url, method="POST", data=b"")
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                resp.read()
                with stats["lock"]:
                    stats["ok"] += 1
        except (urllib.error.URLError, OSError) as exc:
            with stats["lock"]:
                stats["errors"] += 1
            print(f"[worker {worker_id}] request failed: {exc}", flush=True)

        if interval > 0:
            stop_event.wait(interval)


def keep_alive_worker(
    worker_id, host, port, path, stop_event, interval, timeout, stats
):
    conn = http.client.HTTPConnection(host, port, timeout=timeout)
    try:
        while not stop_event.is_set():
            num = random.randint(-100, 100)
            url_path = f"{path}?{urlencode({'num': num})}"
            try:
                conn.request("POST", url_path, body=b"")
                resp = conn.getresponse()
                resp.read()
                with stats["lock"]:
                    stats["ok"] += 1
            except (http.client.HTTPException, OSError) as exc:
                with stats["lock"]:
                    stats["errors"] += 1
                print(f"[worker {worker_id}] request failed: {exc}", flush=True)
                conn.close()
                conn = http.client.HTTPConnection(host, port, timeout=timeout)

            if interval > 0:
                stop_event.wait(interval)
    finally:
        conn.close()


def main():
    parser = argparse.ArgumentParser(description="Load generator for the calculator")
    parser.add_argument(
        "--url", default="http://localhost:8080/calc", help="calculator endpoint"
    )
    parser.add_argument(
        "-n", "--threads", type=int, default=10, help="number of worker threads"
    )
    parser.add_argument(
        "--interval",
        type=float,
        default=0.1,
        help="pause between requests per thread, in seconds (0 = as fast as possible)",
    )
    parser.add_argument(
        "--timeout", type=float, default=5.0, help="HTTP request timeout, seconds"
    )
    parser.add_argument(
        "--no-keep-alive",
        action="store_true",
        help="new connection per request",
    )
    args = parser.parse_args()

    try:
        host, port, path = prepare_url(args.url)
    except Exception as exc:  # noqa: BLE001
        print(f"prepare_url: {exc}")
        sys.exit(1)

    stop_event = threading.Event()
    stats = {"lock": threading.Lock(), "ok": 0, "errors": 0}

    worker = keep_alive_worker
    if args.no_keep_alive:
        worker = orig_worker

    threads = []
    for i in range(args.threads):
        t = threading.Thread(
            target=worker,
            args=(i, host, port, path, stop_event, args.interval, args.timeout, stats),
            daemon=True,
        )
        t.start()
        threads.append(t)

    print(
        f"Generator started: {args.threads} threads -> http://{host}:{port}{path}",
        flush=True,
    )

    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        print("\nSIGINT received, stopping generator...", flush=True)
        stop_event.set()
        for t in threads:
            t.join(timeout=2)
        with stats["lock"]:
            print(f"Total requests: ok={stats['ok']} errors={stats['errors']}")


if __name__ == "__main__":
    main()
