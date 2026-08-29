CREATE TABLE IF NOT EXISTS evaluation_runs (
    id          VARCHAR(128) PRIMARY KEY,
    tenant_id   INTEGER      NOT NULL,
    dataset_id  VARCHAR(128) NOT NULL,
    start_time  DATETIME     NOT NULL,
    end_time    DATETIME,
    status      INTEGER      NOT NULL,
    err_msg     TEXT,
    total       INTEGER      NOT NULL DEFAULT 0,
    finished    INTEGER      NOT NULL DEFAULT 0,
    params      TEXT,
    config      TEXT,
    metric      TEXT,
    cost        TEXT,
    duration    TEXT,
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_evaluation_runs_tenant_started
    ON evaluation_runs (tenant_id, start_time DESC);

CREATE TABLE IF NOT EXISTS evaluation_cases (
    task_id     VARCHAR(128) NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE,
    tenant_id   INTEGER      NOT NULL,
    qid         INTEGER      NOT NULL,
    duration_ms INTEGER      NOT NULL DEFAULT 0,
    stages      TEXT,
    retrieved   TEXT,
    reranked    TEXT,
    final_answer TEXT,
    metric      TEXT,
    err_msg     TEXT,
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (task_id, qid)
);

CREATE INDEX IF NOT EXISTS idx_evaluation_cases_tenant_task
    ON evaluation_cases (tenant_id, task_id);

CREATE TABLE IF NOT EXISTS model_calls (
    id                          VARCHAR(36) PRIMARY KEY,
    tenant_id                   INTEGER      NOT NULL,
    model_id                    VARCHAR(64)  NOT NULL,
    model_type                  VARCHAR(32)  NOT NULL,
    purpose                     VARCHAR(96),
    correlation_id              VARCHAR(128),
    prompt_prefix_fingerprint   VARCHAR(64),
    prompt_tokens               INTEGER      NOT NULL DEFAULT 0,
    completion_tokens           INTEGER      NOT NULL DEFAULT 0,
    total_tokens                INTEGER      NOT NULL DEFAULT 0,
    cache_read_tokens           INTEGER      NOT NULL DEFAULT 0,
    cache_write_tokens          INTEGER      NOT NULL DEFAULT 0,
    cache_miss_tokens           INTEGER      NOT NULL DEFAULT 0,
    input_units                 INTEGER      NOT NULL DEFAULT 0,
    item_count                  INTEGER      NOT NULL DEFAULT 0,
    cache_hit_items             INTEGER      NOT NULL DEFAULT 0,
    cache_miss_items            INTEGER      NOT NULL DEFAULT 0,
    duration_ms                 INTEGER      NOT NULL DEFAULT 0,
    success                     INTEGER      NOT NULL,
    error_code                  VARCHAR(96),
    pricing                     TEXT,
    cost                        REAL         NOT NULL DEFAULT 0,
    cost_estimated              INTEGER      NOT NULL DEFAULT 1,
    created_at                  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_calls_tenant_created
    ON model_calls (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_calls_tenant_model_created
    ON model_calls (tenant_id, model_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_calls_tenant_correlation
    ON model_calls (tenant_id, correlation_id, created_at);

CREATE TABLE IF NOT EXISTS embedding_cache (
    id           VARCHAR(36) PRIMARY KEY,
    tenant_id    INTEGER      NOT NULL,
    model_id     VARCHAR(64)  NOT NULL,
    dimensions   INTEGER      NOT NULL,
    model_fingerprint CHAR(64) NOT NULL,
    content_hash CHAR(64)     NOT NULL,
    vector       TEXT         NOT NULL,
    hit_count    INTEGER      NOT NULL DEFAULT 0,
    created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    accessed_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, model_id, dimensions, model_fingerprint, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_embedding_cache_accessed_at
    ON embedding_cache (accessed_at);
