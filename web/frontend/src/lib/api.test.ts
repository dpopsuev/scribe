import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fetchArtifacts, fetchScopes, ApiError } from './api';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

beforeEach(() => mockFetch.mockReset());

function jsonResponse(data: unknown, status = 200) {
	return new Response(JSON.stringify(data), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

describe('fetchArtifacts', () => {
	it('fetches artifacts with query params', async () => {
		mockFetch.mockResolvedValue(jsonResponse([{ id: '1', title: 'Test' }]));
		const result = await fetchArtifacts({ scope: 'scribe', kind_prefix: 'effort' });
		expect(result).toHaveLength(1);
		expect(result[0].id).toBe('1');

		const url = mockFetch.mock.calls[0][0] as string;
		expect(url).toContain('scope=scribe');
		expect(url).toContain('kind_prefix=effort');
	});

	it('throws ApiError on HTTP error', async () => {
		mockFetch.mockResolvedValue(new Response('not found', { status: 404 }));
		try {
			await fetchArtifacts();
			expect.unreachable('should have thrown');
		} catch (e) {
			expect(e).toBeInstanceOf(ApiError);
			expect((e as ApiError).status).toBe(404);
			expect((e as ApiError).message).toBe('not found');
		}
	});
});

describe('fetchScopes', () => {
	it('fetches scopes list', async () => {
		mockFetch.mockResolvedValue(jsonResponse([{ scope: 'scribe', count: 10 }]));
		const result = await fetchScopes();
		expect(result[0].scope).toBe('scribe');
	});
});

describe('ApiError', () => {
	it('preserves HTTP status code', () => {
		const err = new ApiError(403, 'forbidden');
		expect(err.status).toBe(403);
		expect(err.message).toBe('forbidden');
		expect(err).toBeInstanceOf(Error);
	});
});
