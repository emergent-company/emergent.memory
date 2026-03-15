FROM python:3.12-slim

# Install system deps needed by httpx / common data-science packages
RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        curl \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

# Copy SDK source and install it (editable so the image stays small)
COPY . /sdk
RUN pip install --no-cache-dir /sdk

# Pre-install common helper packages agents are likely to import
RUN pip install --no-cache-dir \
        requests \
        pydantic \
        python-dateutil

# Install the pyrunner daemon for zero-cold-start script execution.
# The daemon pre-imports the emergent SDK and forks a child per request.
COPY pyrunner.py /usr/local/bin/pyrunner.py
RUN chmod +x /usr/local/bin/pyrunner.py

# Entrypoint: start the pyrunner daemon in background, then exec CMD.
# gvisor_provider.go overrides CMD with ["sleep", "infinity"] so the
# daemon starts automatically at container boot regardless of CMD.
ENTRYPOINT ["sh", "-c", "python3 /usr/local/bin/pyrunner.py & exec \"$@\"", "--"]

# Default working directory for agent scripts
WORKDIR /workspace

CMD ["sleep", "infinity"]
