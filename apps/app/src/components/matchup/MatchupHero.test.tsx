import { render, screen } from '@testing-library/react-native';

import { Subline, sublineText } from './MatchupHero';
import type { AnySide, TeamRecord } from '@/lib/matchup';

const side = (name: string, managerName: string): AnySide =>
  ({ kind: 'team', team: { id: 'tm_1', name, managerName } }) as AnySide;

const records = new Map<string, TeamRecord>([['tm_1', { record: '3-2-1', streak: null, rank: 5 }]]);

describe('stacked Subline (matchup cards)', () => {
  it('names the manager as the owner chip does', async () => {
    await render(
      <Subline side={side('Park The Bus FC', 'J Doe')} records={records} align="left" stacked />,
    );
    expect(screen.getByTestId('subline-left-manager')).toHaveTextContent('J Doe');
  });

  it('falls back to the team name, never "Open seat" or an emoji', async () => {
    const { rerender } = await render(
      <Subline
        side={side('Park The Bus FC', 'Open seat')}
        records={records}
        align="left"
        stacked
      />,
    );
    expect(screen.getByTestId('subline-left-manager')).toHaveTextContent('Park The Bus FC');
    await rerender(
      <Subline side={side('Park The Bus FC', '🦁')} records={records} align="left" stacked />,
    );
    expect(screen.getByTestId('subline-left-manager')).toHaveTextContent('Park The Bus FC');
  });

  it('uses the first name when there is no record yet', async () => {
    await render(
      <Subline
        side={side('Park The Bus FC', 'Dr Sam Rivers')}
        records={undefined}
        align="right"
        stacked
      />,
    );
    expect(screen.getByTestId('subline-right')).toHaveTextContent('Sam');
    expect(screen.queryByText('Dr Sam Rivers')).toBeNull();
  });

  it('keeps the full manager name in the wide hero', () => {
    expect(sublineText(side('Park The Bus FC', 'Sam Rivers'), undefined, 'left')).toBe(
      'Sam Rivers',
    );
  });
});
