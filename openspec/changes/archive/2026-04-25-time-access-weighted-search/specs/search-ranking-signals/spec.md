## ADDED Requirements

### Requirement: Recency boost parameter on hybrid search
The hybrid search request SHALL accept an optional `recency_boost` (float32, default 0) and `recency_half_life` (float32, default 168, unit: hours). When `recency_boost > 0`, the server MUST add a recency score to the final ranking: `recency_score = 1 / (1 + exp((hours_old - half_life) / (half_life / 4.0)))` where `hours_old` is hours since `created_at`. The recency score contribution is `recency_boost * recency_score`.

#### Scenario: Recency boost elevates newer objects
- **WHEN** a search is run with `recency_boost=0.5` and two equally-relevant objects exist (one created today, one 30 days ago)
- **THEN** the object created today MUST rank higher

#### Scenario: Zero recency boost preserves existing behavior
- **WHEN** `recency_boost` is omitted or 0
- **THEN** search results MUST be identical to results without the parameter

#### Scenario: Custom half_life shifts decay curve
- **WHEN** `recency_half_life=720` (30 days) is specified
- **THEN** an object 30 days old MUST receive a recency_score of approximately 0.5

### Requirement: Access frequency boost parameter on hybrid search
The hybrid search request SHALL accept an optional `access_boost` (float32, default 0). When `access_boost > 0`, the server MUST compute an access score from the object's analytics: `access_score = LEAST(access_count / 100.0, 1.0) * 0.5 + GREATEST(0, 1 - days_since_access / 365.0) * 0.5`. Objects with no analytics record MUST receive `access_score = 0`.

#### Scenario: Access boost elevates frequently-accessed objects
- **WHEN** search is run with `access_boost=0.3` and one object has access_count=50 vs another with 0
- **THEN** the object with 50 accesses MUST rank higher (all else equal)

#### Scenario: Zero access boost preserves existing behavior
- **WHEN** `access_boost` is omitted or 0
- **THEN** search results MUST be identical to baseline

### Requirement: CLI exposes ranking signal flags
The CLI `memory query` command SHALL accept `--recency-boost <float>`, `--recency-half-life <float>`, and `--access-boost <float>` flags that map to the corresponding API parameters.

#### Scenario: CLI passes flags to API
- **WHEN** `memory query "topic" --recency-boost 0.3` is run
- **THEN** the API request MUST include `recencyBoost: 0.3`
