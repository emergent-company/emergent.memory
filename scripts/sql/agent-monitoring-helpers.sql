-- ============================================================================
-- Agent Run Monitoring Helper Scripts
-- ============================================================================
-- Use these SQL queries to monitor agent runs, diagnose issues, and manage
-- stuck runs while the frontend UI is being updated with full visibility.
--
-- Usage: Connect to the database and run these queries as needed.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 1. AGENT CONFIGURATION: View limits and settings
-- ----------------------------------------------------------------------------

-- View all agent definitions with their limits
SELECT 
  id,
  name,
  max_steps,
  default_timeout,
  model->>'name' as model_name,
  (model->>'maxTokens')::int as max_tokens,
  (model->>'temperature')::float as temperature,
  visibility,
  created_at
FROM kb.agent_definitions
ORDER BY created_at DESC;

-- Get specific agent configuration
SELECT 
  id,
  name,
  max_steps,
  default_timeout,
  model,
  tools,
  system_prompt
FROM kb.agent_definitions
WHERE id = 'YOUR-AGENT-DEF-ID';

-- ----------------------------------------------------------------------------
-- 2. ACTIVE RUNS: Monitor currently running agents
-- ----------------------------------------------------------------------------

-- List all currently running agents with execution details
SELECT 
  r.id as run_id,
  a.name as agent_name,
  r.status,
  r.step_count || ' / ' || COALESCE(r.max_steps::text, '∞') as steps,
  r.started_at,
  NOW() - r.started_at as running_duration,
  EXTRACT(EPOCH FROM (NOW() - r.started_at))::int || 's' as duration_seconds,
  r.parent_run_id,
  r.resumed_from
FROM kb.agent_runs r
JOIN kb.agents a ON a.id = r.agent_id
WHERE r.status = 'running'
ORDER BY r.started_at DESC;

-- Find potentially stuck runs (running > 1 hour)
SELECT 
  r.id as run_id,
  a.name as agent_name,
  r.step_count,
  r.max_steps,
  r.started_at,
  NOW() - r.started_at as running_duration,
  ROUND(EXTRACT(EPOCH FROM (NOW() - r.started_at)) / 60) || ' minutes' as minutes_running
FROM kb.agent_runs r
JOIN kb.agents a ON a.id = r.agent_id
WHERE r.status = 'running'
  AND NOW() - r.started_at > INTERVAL '1 hour'
ORDER BY r.started_at;

-- ----------------------------------------------------------------------------
-- 3. RUN HISTORY: View completed/failed runs
-- ----------------------------------------------------------------------------

-- Recent run history for all agents
SELECT 
  r.id as run_id,
  a.name as agent_name,
  r.status,
  r.step_count,
  r.max_steps,
  r.started_at,
  r.completed_at,
  COALESCE(r.duration_ms::text || 'ms', 
           EXTRACT(EPOCH FROM (COALESCE(r.completed_at, NOW()) - r.started_at))::int || 's') as duration,
  r.error_message,
  r.skip_reason
FROM kb.agent_runs r
JOIN kb.agents a ON a.id = r.agent_id
ORDER BY r.started_at DESC
LIMIT 20;

-- Runs for a specific agent
SELECT 
  r.id as run_id,
  r.status,
  r.step_count || ' / ' || COALESCE(r.max_steps::text, '∞') as steps,
  r.started_at,
  r.completed_at,
  r.duration_ms,
  r.error_message
FROM kb.agent_runs r
WHERE r.agent_id = 'YOUR-AGENT-ID'
ORDER BY r.started_at DESC
LIMIT 10;

-- Run success/failure statistics per agent
SELECT 
  a.name as agent_name,
  COUNT(*) as total_runs,
  SUM(CASE WHEN r.status = 'success' THEN 1 ELSE 0 END) as successful,
  SUM(CASE WHEN r.status = 'error' THEN 1 ELSE 0 END) as errors,
  SUM(CASE WHEN r.status = 'paused' THEN 1 ELSE 0 END) as paused,
  SUM(CASE WHEN r.status = 'cancelled' THEN 1 ELSE 0 END) as cancelled,
  ROUND(AVG(r.step_count), 1) as avg_steps,
  ROUND(AVG(r.duration_ms) / 1000, 1) as avg_duration_seconds
FROM kb.agent_runs r
JOIN kb.agents a ON a.id = r.agent_id
WHERE r.started_at > NOW() - INTERVAL '7 days'
GROUP BY a.id, a.name
ORDER BY total_runs DESC;

-- ----------------------------------------------------------------------------
-- 4. MESSAGES & TOOL CALLS: Debug run execution
-- ----------------------------------------------------------------------------

-- Get conversation history for a run
SELECT 
  id,
  role,
  step_number,
  content,
  created_at
FROM kb.agent_run_messages
WHERE run_id = 'YOUR-RUN-ID'
ORDER BY step_number, created_at;

-- Get tool calls for a run
SELECT 
  tool_name,
  status,
  duration_ms,
  step_number,
  created_at,
  input,
  output
FROM kb.agent_run_tool_calls
WHERE run_id = 'YOUR-RUN-ID'
ORDER BY step_number, created_at;

-- Tool call statistics for a run
SELECT 
  tool_name,
  COUNT(*) as call_count,
  SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed,
  SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as errors,
  ROUND(AVG(duration_ms), 1) as avg_duration_ms,
  MAX(duration_ms) as max_duration_ms
FROM kb.agent_run_tool_calls
WHERE run_id = 'YOUR-RUN-ID'
GROUP BY tool_name
ORDER BY call_count DESC;

-- Check for doom loop (consecutive identical tool calls)
WITH consecutive_calls AS (
  SELECT 
    tool_name,
    input,
    step_number,
    LAG(tool_name) OVER (ORDER BY step_number, created_at) as prev_tool,
    LAG(input) OVER (ORDER BY step_number, created_at) as prev_input
  FROM kb.agent_run_tool_calls
  WHERE run_id = 'YOUR-RUN-ID'
  ORDER BY step_number, created_at
)
SELECT 
  tool_name,
  input,
  COUNT(*) as consecutive_count
FROM consecutive_calls
WHERE tool_name = prev_tool 
  AND input::text = prev_input::text
GROUP BY tool_name, input
HAVING COUNT(*) >= 3
ORDER BY consecutive_count DESC;

-- ----------------------------------------------------------------------------
-- 5. EMERGENCY OPERATIONS: Cancel or fix stuck runs
-- ----------------------------------------------------------------------------

-- Cancel a specific stuck run
UPDATE kb.agent_runs
SET 
  status = 'cancelled',
  completed_at = NOW(),
  duration_ms = EXTRACT(EPOCH FROM (NOW() - started_at)) * 1000,
  error_message = 'Manually cancelled via SQL - stuck for ' || 
                  ROUND(EXTRACT(EPOCH FROM (NOW() - started_at)) / 60) || ' minutes'
WHERE id = 'YOUR-RUN-ID'
  AND status = 'running'
RETURNING id, status, error_message;

-- Bulk cancel all runs stuck for more than 2 hours
UPDATE kb.agent_runs
SET 
  status = 'cancelled',
  completed_at = NOW(),
  duration_ms = EXTRACT(EPOCH FROM (NOW() - started_at)) * 1000,
  error_message = 'Auto-cancelled - exceeded 2 hour timeout'
WHERE status = 'running'
  AND NOW() - started_at > INTERVAL '2 hours'
RETURNING id, agent_id, started_at, error_message;

-- Mark paused runs as failed if they'll never resume
UPDATE kb.agent_runs
SET 
  status = 'error',
  completed_at = NOW(),
  duration_ms = EXTRACT(EPOCH FROM (NOW() - started_at)) * 1000,
  error_message = 'Paused run abandoned - exceeded maximum age'
WHERE status = 'paused'
  AND NOW() - started_at > INTERVAL '24 hours'
RETURNING id, agent_id, step_count, max_steps;

-- ----------------------------------------------------------------------------
-- 6. CONFIGURATION UPDATES: Set limits via SQL
-- ----------------------------------------------------------------------------

-- Set step limit and timeout for a specific agent definition
UPDATE kb.agent_definitions
SET 
  max_steps = 100,                  -- Limit to 100 steps
  default_timeout = 1200,           -- 20 minutes timeout
  updated_at = NOW()
WHERE id = 'YOUR-AGENT-DEF-ID'
RETURNING id, name, max_steps, default_timeout;

-- Set max tokens for a specific agent definition
UPDATE kb.agent_definitions
SET 
  model = jsonb_set(
    COALESCE(model, '{}'::jsonb), 
    '{maxTokens}', 
    '4096'::jsonb
  ),
  updated_at = NOW()
WHERE id = 'YOUR-AGENT-DEF-ID'
RETURNING id, name, model;

-- Set all configuration options at once
UPDATE kb.agent_definitions
SET 
  max_steps = 100,
  default_timeout = 1200,
  model = jsonb_set(
    jsonb_set(
      COALESCE(model, '{}'::jsonb),
      '{maxTokens}',
      '4096'::jsonb
    ),
    '{temperature}',
    '0.7'::jsonb
  ),
  updated_at = NOW()
WHERE id = 'YOUR-AGENT-DEF-ID'
RETURNING id, name, max_steps, default_timeout, model;

-- ----------------------------------------------------------------------------
-- 7. USEFUL VIEWS: Create convenient views for monitoring
-- ----------------------------------------------------------------------------

-- Create a view for active agent monitoring
CREATE OR REPLACE VIEW kb.v_active_agent_runs AS
SELECT 
  r.id as run_id,
  a.name as agent_name,
  r.status,
  r.step_count,
  r.max_steps,
  ROUND(r.step_count::numeric / NULLIF(r.max_steps, 0) * 100, 1) as step_progress_pct,
  r.started_at,
  r.completed_at,
  NOW() - r.started_at as running_duration,
  ROUND(EXTRACT(EPOCH FROM (NOW() - r.started_at))) as duration_seconds,
  r.duration_ms,
  r.error_message,
  r.parent_run_id,
  r.resumed_from
FROM kb.agent_runs r
JOIN kb.agents a ON a.id = r.agent_id
WHERE r.status IN ('running', 'paused');

-- Use the view
SELECT * FROM kb.v_active_agent_runs
ORDER BY started_at DESC;

-- Create a view for run summaries
CREATE OR REPLACE VIEW kb.v_agent_run_summary AS
SELECT 
  r.id,
  r.agent_id,
  a.name as agent_name,
  r.status,
  r.step_count,
  r.max_steps,
  r.started_at,
  r.completed_at,
  r.duration_ms,
  (SELECT COUNT(*) FROM kb.agent_run_messages WHERE run_id = r.id) as message_count,
  (SELECT COUNT(*) FROM kb.agent_run_tool_calls WHERE run_id = r.id) as tool_call_count,
  (SELECT COUNT(*) FROM kb.agent_run_tool_calls WHERE run_id = r.id AND status = 'error') as tool_error_count
FROM kb.agent_runs r
JOIN kb.agents a ON a.id = r.agent_id;

-- Use the view
SELECT * FROM kb.v_agent_run_summary
WHERE started_at > NOW() - INTERVAL '1 day'
ORDER BY started_at DESC;

-- ----------------------------------------------------------------------------
-- USAGE EXAMPLES
-- ----------------------------------------------------------------------------

/*
Example 1: Monitor all active runs
-----------------------------------
SELECT * FROM kb.v_active_agent_runs;

Example 2: Check if an agent has proper limits configured
----------------------------------------------------------
SELECT id, name, max_steps, default_timeout, model
FROM kb.agent_definitions
WHERE name = 'Janitor Agent';

Example 3: Cancel a stuck run
------------------------------
UPDATE kb.agent_runs
SET status = 'cancelled', 
    completed_at = NOW(),
    error_message = 'Manually cancelled - stuck'
WHERE id = '123e4567-e89b-12d3-a456-426614174000'
RETURNING id, status;

Example 4: View recent tool calls for debugging
------------------------------------------------
SELECT tool_name, status, duration_ms, step_number
FROM kb.agent_run_tool_calls
WHERE run_id = '123e4567-e89b-12d3-a456-426614174000'
ORDER BY step_number;

Example 5: Set limits on agent definition
------------------------------------------
UPDATE kb.agent_definitions
SET max_steps = 100, default_timeout = 900
WHERE name = 'Janitor Agent'
RETURNING id, name, max_steps, default_timeout;
*/
