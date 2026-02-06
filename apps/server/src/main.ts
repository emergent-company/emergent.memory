/**
 * Simple Main Entry Point
 *
 * Simplified version that directly imports main-bootstrap without OTEL complexity.
 */

// Simple require of main-bootstrap - no dynamic imports, no OTEL
console.log('[main] Loading main-bootstrap...');
require('./main-bootstrap');
console.log('[main] main-bootstrap loaded');
