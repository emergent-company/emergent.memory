# NovaPay Platform — Technical Design Specification

**Version:** 3.1  
**Authors:** Jamie Osei (Platform Architect), Ravi Krishnamurthy (Backend Lead), Ola Bergström (Security Lead)  
**Date:** February 2025  
**Status:** Approved

---

## 1. Executive Summary

NovaPay is a real-time payment processing platform built to handle 10 million
transactions per day across 45 countries. This document specifies the design
decisions, component architecture, and integration contracts for the v3.1
release, which introduces multi-currency settlement and a new fraud detection
subsystem.

---

## 2. Stakeholders

| Name                 | Role                    | Responsibility                      |
|----------------------|-------------------------|-------------------------------------|
| Jamie Osei           | Platform Architect      | Overall design authority            |
| Ravi Krishnamurthy   | Backend Lead            | API, worker, and database layers     |
| Ola Bergström        | Security Lead           | Fraud detection, compliance         |
| Nadia Popescu        | Product Manager         | Requirements and acceptance criteria|
| Kwame Asante         | Infrastructure Lead     | Kubernetes, networking, deployment  |

---

## 3. Architecture Decisions

### ADR-101: Adopt Event Sourcing for Transaction Records

**Decided by:** Jamie Osei  
**Status:** Accepted  
**Rationale:** Event sourcing provides an immutable audit trail required by
PCI-DSS compliance. All state changes are captured as domain events. The event
store is Kafka with long-term compaction to PostgreSQL.

### ADR-102: Use CQRS for Transaction Query API

**Decided by:** Ravi Krishnamurthy  
**Status:** Accepted  
**Rationale:** Separating the read and write models allows independent scaling.
The query side uses a denormalized view updated by event projections.

### ADR-103: Implement ML-Based Fraud Detection

**Decided by:** Ola Bergström with sign-off from Nadia Popescu  
**Status:** Accepted  
**Rationale:** Rule-based systems produced a 23% false-positive rate in
production. The new gradient boosting model (XGBoost) reduces this to 4.1%
while maintaining 98.7% fraud recall in A/B testing.

---

## 4. Component Design

### 4.1 Payment Processor

The payment processor is a stateless service written in Go. It:
- Validates incoming payment requests against JSON Schema
- Calls the fraud detection service synchronously (p99 < 20ms SLA)
- Writes domain events to Kafka
- Returns an idempotency-safe response to the caller

### 4.2 Fraud Detection Service

The fraud detection service is a Python FastAPI application:
- Loads the XGBoost model from S3 on startup
- Computes 47 features from transaction context
- Returns a risk score (0.0–1.0) and a decision (allow/review/block)
- Ola Bergström owns the model training pipeline

### 4.3 Settlement Engine

The settlement engine processes end-of-day batches:
- Groups transactions by currency and counterparty
- Computes net positions using ISO 4217 currency codes
- Submits SWIFT MT103 messages to the interbank network
- Kwame Asante manages the infrastructure for the settlement cluster

---

## 5. Data Models

### Transaction Event

```json
{
  "event_id": "uuid",
  "event_type": "transaction.initiated",
  "occurred_at": "ISO-8601",
  "payload": {
    "transaction_id": "uuid",
    "amount_minor_units": 12500,
    "currency": "USD",
    "payer_account": "uuid",
    "payee_account": "uuid",
    "fraud_score": 0.03
  }
}
```

---

## 6. Security and Compliance

All API endpoints require OAuth 2.0 Bearer tokens. The fraud model training
data is anonymised per GDPR Article 25. PCI-DSS Level 1 certification is
maintained through annual QSA audit. Ola Bergström is the designated QSA
liaison and conducts quarterly penetration tests.

---

## 7. Open Items

1. Ravi Krishnamurthy to finalise database sharding strategy by March 1.
2. Kwame Asante to provision Kubernetes namespaces for settlement cluster.
3. Nadia Popescu to confirm acceptance criteria for multi-currency UI by Feb 28.
