import type { Schemas } from '@myfantasyprem/api-client';

import { carouselEntries, groupByTeam, tradeHref, wantedText } from './trade-block';

const entry = (player: string, team: string, name: string, addedAt: string) =>
  ({
    player: { id: player },
    team: { id: team, name, managerName: 'M' },
    note: '',
    wanted: [],
    addedAt,
  }) as unknown as Schemas['TradeBlockEntry'];

const block = [
  entry('p1', 'tm_b', 'Bravo', '2026-10-14T10:00:00Z'),
  entry('p2', 'tm_me', 'Mine', '2026-10-14T11:00:00Z'),
  entry('p3', 'tm_a', 'Alpha', '2026-10-15T09:00:00Z'),
  entry('p4', 'tm_b', 'Bravo', '2026-10-15T12:00:00Z'),
];

describe('trade block', () => {
  it('groups by team: mine first, then by name, players in block order', () => {
    expect(
      groupByTeam(block, 'tm_me').map((g) => `${g.team.name}:${g.entries.map((e) => e.player.id)}`),
    ).toEqual(['Mine:p2', 'Alpha:p3', 'Bravo:p1,p4']);
    expect(groupByTeam(block).map((g) => g.team.name)).toEqual(['Alpha', 'Bravo', 'Mine']);
  });

  it("puts other teams' newest players first in the carousel", () => {
    expect(carouselEntries(block, 'tm_me').map((e) => e.player.id)).toEqual(['p4', 'p3', 'p1']);
  });

  it('says what the team wants', () => {
    expect(wantedText(['GKP', 'MID'])).toBe('Wants GKP, MID');
    expect(wantedText([])).toBe('Open to anything');
  });

  it('opens the trade builder on the team with the player asked for', () => {
    expect(tradeHref('lg_1', block[0]!)).toBe(
      '/leagues/lg_1/players/trades/new?teamId=tm_b&playerId=p1',
    );
  });
});
