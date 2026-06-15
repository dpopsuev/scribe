export const STATUS_LANES: Record<string, string[]> = {
	effort: ['work.draft', 'work.active', 'work.blocked', 'work.complete', 'cancelled'],
	intent: ['work.draft', 'decision.proposed', 'decision.accepted', 'decision.rejected', 'archived'],
	knowledge: ['note.fleeting', 'note.mature', 'note.evergreen', 'archived'],
};

export function statusesFor(kindPrefix: string): string[] {
	return STATUS_LANES[kindPrefix] ?? STATUS_LANES.effort;
}

export function statusLabel(s: string): string {
	return s.split('.').pop()?.replace(/_/g, ' ') ?? s;
}
