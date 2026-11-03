#!/usr/bin/env python3
"""Headless Dashboard helper restricted to the local URSim instance."""

import argparse
import socket
import time


class Dashboard:
    def __enter__(self):
        self.socket = socket.create_connection(("127.0.0.1", 29999), timeout=3)
        self.reader = self.socket.makefile("rb")
        greeting = self.reader.readline().decode().strip()
        if "Dashboard Server" not in greeting:
            self.__exit__(None, None, None)
            raise RuntimeError(f"Unexpected Dashboard greeting: {greeting}")
        return self

    def __exit__(self, *_):
        self.reader.close()
        self.socket.close()

    def command(self, command):
        self.socket.sendall((command + "\n").encode())
        reply = self.reader.readline().decode().strip()
        if not reply:
            raise RuntimeError(f"Dashboard closed during {command!r}")
        return reply

    def wait_mode(self, mode, timeout=40):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            reply = self.command("robotmode")
            if reply.upper() == f"ROBOTMODE: {mode}":
                return reply
            time.sleep(0.25)
        raise RuntimeError(f"Expected {mode}; last Dashboard reply: {reply}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["wait", "status", "ready", "stop"])
    args = parser.parse_args()
    if args.action == "wait":
        deadline = time.monotonic() + 120
        last_error = None
        while time.monotonic() < deadline:
            try:
                with Dashboard() as dashboard:
                    version = dashboard.command("PolyscopeVersion")
                    mode = dashboard.command("robotmode").upper()
                    safety = dashboard.command("safetystatus").upper()
                    if mode not in ("ROBOTMODE: POWER_OFF", "ROBOTMODE: IDLE", "ROBOTMODE: RUNNING"):
                        raise RuntimeError(f"Controller still initializing: {mode}")
                    if safety != "SAFETYSTATUS: NORMAL":
                        raise RuntimeError(f"Controller not ready: {safety}")
                    print(version)
                    return
            except (OSError, RuntimeError) as error:
                last_error = error
                time.sleep(1)
        raise SystemExit(f"URSim Dashboard not ready after 120s: {last_error}")

    with Dashboard() as dashboard:
        if args.action == "ready":
            safety = dashboard.command("safetystatus")
            print(safety)
            if safety.upper() != "SAFETYSTATUS: NORMAL":
                raise RuntimeError(f"Refusing to power on with {safety}")
            mode = dashboard.command("robotmode").upper()
            if mode not in ("ROBOTMODE: IDLE", "ROBOTMODE: RUNNING"):
                print(dashboard.command("power on"))
                print(dashboard.wait_mode("IDLE"))
            if mode != "ROBOTMODE: RUNNING":
                print(dashboard.command("brake release"))
                print(dashboard.wait_mode("RUNNING"))
        elif args.action == "stop":
            print(dashboard.command("stop"))
        else:
            for command in ("PolyscopeVersion", "is in remote control", "robotmode", "safetystatus"):
                print(f"{command}: {dashboard.command(command)}")


if __name__ == "__main__":
    main()
