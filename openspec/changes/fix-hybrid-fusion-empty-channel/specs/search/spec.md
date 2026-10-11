## ADDED Requirements

### Requirement: Single-channel fusion does not penalise the surviving channel

Hybrid search fuses lexical and vector scores with a weighted sum. When exactly one channel produced no scores and the other produced some, the surviving channel SHALL receive effective weight `1.0` and the empty channel `0.0` before fusing. When both channels are non-empty, or both are empty, the weights SHALL remain unchanged.

#### Scenario: Lexical empty, vector present

- **WHEN** hybrid search runs and the lexical channel produces no candidates but the vector channel produces some
- **THEN** the vector channel SHALL be fused with effective weight `1.0` and the lexical channel `0.0`
- **AND** vector candidates MUST NOT be scaled down by the discarded lexical weight

#### Scenario: Vector empty, lexical present

- **WHEN** hybrid search runs and the vector channel produces no candidates but the lexical channel produces some
- **THEN** the lexical channel SHALL be fused with effective weight `1.0` and the vector channel `0.0`
- **AND** lexical candidates MUST NOT be scaled down by the discarded vector weight

#### Scenario: Both channels present

- **WHEN** hybrid search runs and both lexical and vector channels produce candidates
- **THEN** the configured weights SHALL be used unchanged

#### Scenario: Both channels empty

- **WHEN** hybrid search runs and neither channel produces candidates
- **THEN** the configured weights SHALL be used unchanged and no candidates SHALL be fused
