package installer

const serverImage = "ghcr.io/emergent-company/emergent-server-go:latest"

func GetDockerComposeTemplate() string {
	return `services:
  db:
    image: pgvector/pgvector:pg16
    container_name: emergent-db
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-emergent}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
      POSTGRES_DB: ${POSTGRES_DB:-emergent}
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./init.sql:/docker-entrypoint-initdb.d/00-init.sql:ro
    ports:
      - '${POSTGRES_PORT:-5432}:5432'
    healthcheck:
      test: ['CMD-SHELL', 'pg_isready -U ${POSTGRES_USER:-emergent} -d ${POSTGRES_DB:-emergent}']
      interval: 5s
      timeout: 5s
      retries: 10
    networks:
      - emergent

  kreuzberg:
    image: goldziher/kreuzberg:latest
    container_name: emergent-kreuzberg
    restart: unless-stopped
    ports:
      - '${KREUZBERG_PORT:-8000}:8000'
    environment:
      - LOG_LEVEL=${KREUZBERG_LOG_LEVEL:-info}
    healthcheck:
      test: ['CMD', 'curl', '-f', 'http://localhost:8000/health']
      interval: 30s
      timeout: 10s
      retries: 3
    deploy:
      resources:
        limits:
          memory: 2G
        reservations:
          memory: 512M
    networks:
      - emergent

  minio:
    image: minio/minio:latest
    container_name: emergent-minio
    restart: unless-stopped
    command: server /data --console-address ':9001'
    environment:
      MINIO_ROOT_USER: ${MINIO_ROOT_USER:-minioadmin}
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:-changeme}
    ports:
      - '${MINIO_API_PORT:-9000}:9000'
    volumes:
      - minio_data:/data
    healthcheck:
      test: ['CMD', 'curl', '-f', 'http://localhost:9000/minio/health/live']
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - emergent

  minio-init:
    image: minio/mc:latest
    container_name: emergent-minio-init
    depends_on:
      minio:
        condition: service_healthy
    entrypoint: >
      /bin/sh -c "
      sleep 2;
      /usr/bin/mc alias set myminio http://minio:9000 $${MINIO_ROOT_USER:-minioadmin} $${MINIO_ROOT_PASSWORD:-changeme};
      /usr/bin/mc mb myminio/documents --ignore-existing;
      /usr/bin/mc mb myminio/document-temp --ignore-existing;
      /usr/bin/mc mb myminio/backups --ignore-existing;
      echo 'MinIO buckets initialized';
      exit 0;
      "
    networks:
      - emergent

  server:
    image: ` + serverImage + `
    container_name: emergent-server
    restart: unless-stopped
    ports:
      - '${SERVER_PORT:-3002}:3002'
    volumes:
      - emergent_cli_config:/root/.emergent
    environment:
      STANDALONE_MODE: 'true'
      STANDALONE_API_KEY: ${STANDALONE_API_KEY}
      STANDALONE_USER_EMAIL: ${STANDALONE_USER_EMAIL}
      STANDALONE_ORG_NAME: ${STANDALONE_ORG_NAME}
      STANDALONE_PROJECT_NAME: ${STANDALONE_PROJECT_NAME}
      POSTGRES_HOST: db
      POSTGRES_PORT: 5432
      POSTGRES_USER: ${POSTGRES_USER:-emergent}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
      POSTGRES_DB: ${POSTGRES_DB:-emergent}
      PORT: 3002
      GO_ENV: production
      KREUZBERG_SERVICE_URL: http://kreuzberg:8000
      KREUZBERG_ENABLED: 'true'
      STORAGE_PROVIDER: minio
      STORAGE_ENDPOINT: http://minio:9000
      STORAGE_ACCESS_KEY: ${MINIO_ROOT_USER:-minioadmin}
      STORAGE_SECRET_KEY: ${MINIO_ROOT_PASSWORD:-changeme}
      STORAGE_BUCKET_DOCUMENTS: documents
      STORAGE_BUCKET_TEMP: document-temp
      STORAGE_USE_SSL: 'false'
      GOOGLE_API_KEY: ${GOOGLE_API_KEY:-}
      EMBEDDING_DIMENSION: ${EMBEDDING_DIMENSION:-768}
      DB_AUTOINIT: 'true'
      SCOPES_DISABLED: 'true'
    depends_on:
      db:
        condition: service_healthy
      kreuzberg:
        condition: service_healthy
      minio:
        condition: service_healthy
    healthcheck:
      test: ['CMD', 'curl', '-f', 'http://localhost:3002/health']
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - emergent

volumes:
  postgres_data:
  minio_data:
  emergent_cli_config:

networks:
  emergent:
`
}

func GetInitSQLTemplate() string {
	return `-- PostgreSQL Initialization Script for Emergent Standalone
-- Creates required extensions and roles

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app_rls') THEN
        CREATE ROLE app_rls WITH NOLOGIN;
    END IF;
END
$$;
`
}
