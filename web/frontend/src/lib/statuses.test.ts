import { describe, it, expect } from 'vitest';
import { statusesFor, statusLabel } from './statuses';

describe('statusesFor', () => {
	it('returns effort statuses for effort prefix', () => {
		const statuses = statusesFor('effort');
		expect(statuses).toContain('work.draft');
		expect(statuses).toContain('work.active');
		expect(statuses).toContain('work.complete');
	});

	it('returns intent statuses for intent prefix', () => {
		const statuses = statusesFor('intent');
		expect(statuses).toContain('decision.proposed');
		expect(statuses).toContain('decision.accepted');
	});

	it('returns knowledge statuses for knowledge prefix', () => {
		const statuses = statusesFor('knowledge');
		expect(statuses).toContain('note.fleeting');
		expect(statuses).toContain('note.evergreen');
	});

	it('falls back to effort for unknown prefix', () => {
		expect(statusesFor('unknown')).toEqual(statusesFor('effort'));
	});
});

describe('statusLabel', () => {
	it('extracts last segment after dot', () => {
		expect(statusLabel('work.draft')).toBe('draft');
		expect(statusLabel('decision.proposed')).toBe('proposed');
		expect(statusLabel('note.fleeting')).toBe('fleeting');
	});

	it('replaces underscores with spaces', () => {
		expect(statusLabel('work.in_progress')).toBe('in progress');
	});

	it('handles single-segment status', () => {
		expect(statusLabel('cancelled')).toBe('cancelled');
		expect(statusLabel('archived')).toBe('archived');
	});
});
