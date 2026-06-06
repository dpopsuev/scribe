/**
 * logger.js — Minimal structured logger for graph modules.
 *
 * Usage:
 *   import { createLogger } from './logger.js';
 *   const log = createLogger('graph');
 *   log.info('loadMacro nodes=%d', 85);
 *   log.error('fetch failed error=%s', err.message);
 *
 * Level order: debug < info < warn < error.
 * Set window.__GRAPH_LOG_LEVEL = 'debug' in console to see debug output.
 */

const LEVELS = { debug: 0, info: 1, warn: 2, error: 3 };

function minLevel() {
  const override = typeof window !== 'undefined' && window.__GRAPH_LOG_LEVEL;
  return LEVELS[override] ?? LEVELS.info;
}

export function createLogger(name) {
  const prefix = `[${name}]`;
  return {
    debug: (...args) => LEVELS.debug >= minLevel() && console.debug(prefix, ...args),
    info:  (...args) => LEVELS.info  >= minLevel() && console.log(prefix,   ...args),
    warn:  (...args) => LEVELS.warn  >= minLevel() && console.warn(prefix,  ...args),
    error: (...args) =>                               console.error(prefix, ...args),
  };
}
