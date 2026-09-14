"""Entry point: run the bridge worker (`python -m memory_bridge start`)."""

import logging

from livekit.agents import cli

from .worker import server

if __name__ == "__main__":
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    cli.run_app(server)
