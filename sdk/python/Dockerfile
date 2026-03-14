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

# Default working directory for agent scripts
WORKDIR /workspace

CMD ["python3"]
