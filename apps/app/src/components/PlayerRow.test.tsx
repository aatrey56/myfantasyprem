import type { Schemas } from '@myfantasyprem/api-client';
import { fireEvent, render, screen } from '@testing-library/react-native';

import {
  formChangeText,
  listMicroStats,
  PlayerRow,
  positionStats,
  RowAction,
  sortedStat,
  sortLabel,
  waiverDay,
  type MicroStat,
} from './PlayerRow';

const item = { totalPoints: 36, gameweekPoints: 7, form: 5.333, minutes: 540 };

describe('sortedStat', () => {
  it('picks the sorted stat, form to one decimal', () => {
    expect(sortedStat(item, 'total_points')).toBe(36);
    expect(sortedStat(item, 'gameweek_points')).toBe(7);
    expect(sortedStat(item, 'form')).toBe(5.3);
    expect(sortedStat({ ...item, form: null }, 'form')).toBeNull();
  });

  it('labels the box in caps', () => {
    expect(sortLabel('total_points')).toBe('PTS');
    expect(sortLabel('form')).toBe('FORM');
    expect(sortLabel('gameweek_points')).toBe('GW');
  });
});

describe('listMicroStats', () => {
  it('lists the stats other than the sorted one', () => {
    expect(listMicroStats(item, 'total_points')).toEqual([
      { label: 'GW PTS', value: '7' },
      { label: 'FORM', value: '5.3' },
      { label: 'MINS', value: '540' },
      { label: 'PTS/90', value: '6.0' },
    ]);
    expect(listMicroStats(item, 'form').map((s) => s.label)).toEqual([
      'PTS',
      'GW PTS',
      'MINS',
      'PTS/90',
    ]);
  });

  it('shows a dash for no form and no minutes', () => {
    const stats = listMicroStats({ ...item, form: null, minutes: 0 }, 'gameweek_points');
    expect(stats.find((s) => s.label === 'FORM')?.value).toBe('–');
    expect(stats.find((s) => s.label === 'PTS/90')?.value).toBe('–');
  });
});

describe('waiverDay', () => {
  const NOW = Date.parse('2026-10-13T08:00:00Z');
  it('is the short weekday the waiver clears, in the given zone', () => {
    expect(waiverDay('2026-10-17T10:00:00Z', NOW, 'Europe/London')).toBe('Sat');
    expect(waiverDay('2026-10-17T23:30:00Z', NOW, 'Europe/London')).toBe('Sun');
    // Beyond six days the kickoff format adds the date; still only the weekday.
    expect(waiverDay('2026-10-24T10:00:00Z', NOW, 'Europe/London')).toBe('Sat');
  });

  it('is empty without a time', () => {
    expect(waiverDay(null, NOW)).toBe('');
    expect(waiverDay('not a date', NOW)).toBe('');
  });
});

describe('PlayerRow list look', () => {
  const player: Schemas['PlayerRef'] = {
    id: 'plr_1',
    displayName: 'C. Strand',
    club: { code: 'NOR', name: 'Northgate' },
    position: 'MID',
    availability: 'available',
    news: null,
  };

  it('shows the rank, position and club, and the points in a labelled box', async () => {
    await render(
      <PlayerRow
        player={player}
        avatar
        rank={3}
        points={36}
        pointsLabel="PTS"
        detail="Free agent"
      />,
    );
    expect(screen.getByTestId('player-rank')).toHaveTextContent('3');
    expect(screen.getByText('MID')).toBeTruthy();
    expect(screen.getByText('NOR')).toBeTruthy();
    expect(screen.getByText('PTS')).toBeTruthy();
    expect(screen.getByTestId('player-points')).toHaveTextContent('36');
    expect(screen.getByText('Free agent')).toBeTruthy();
  });

  it('keeps the default look without `avatar`', async () => {
    await render(<PlayerRow player={player} points={36} pointsLabel="pts" />);
    expect(screen.queryByTestId('player-rank')).toBeNull();
    expect(screen.getByLabelText('Position MID')).toBeTruthy();
  });
});

describe('positionStats', () => {
  const stats: Schemas['PlayerSeasonStats'] = {
    goals: 4,
    assists: 2,
    cleanSheets: 3,
    saves: 21,
    defensiveContributionPoints: 6,
    expectedGoalInvolvements: 5.27,
  };
  const item = (
    position: Schemas['Position'],
    over: Partial<Schemas['PlayerSeasonStats']> = {},
  ) => ({
    player: {
      id: 'plr_1',
      displayName: 'X',
      club: { code: 'NOR', name: 'Northgate' },
      position,
      availability: 'available' as const,
    },
    totalPoints: 52,
    gameweekPoints: 6,
    form: 5.333,
    seasonStats: { ...stats, ...over },
  });
  const strip = (s: MicroStat[]) => s.map((x) => `${x.label} ${x.value}`);

  it('shows keepers saves and clean sheets, defenders clean sheets and DC, then points', () => {
    expect(strip(positionStats(item('GKP'), 'form'))).toEqual(['SV 21', 'CS 3', 'PTS 52']);
    expect(strip(positionStats(item('DEF'), 'gameweek_points'))).toEqual([
      'CS 3',
      'DC 6',
      'PTS 52',
    ]);
  });

  it('shows midfielders and forwards xGI, goals and assists', () => {
    expect(strip(positionStats(item('MID')))).toEqual(['xGI 5.3', 'G 4', 'A 2', 'PTS 52']);
    expect(strip(positionStats(item('FWD', { expectedGoalInvolvements: null })))).toEqual([
      'xGI –',
      'G 4',
      'A 2',
      'PTS 52',
    ]);
  });

  it('ends with form when the box already shows the season points', () => {
    expect(strip(positionStats(item('DEF'), 'total_points'))).toEqual(['CS 3', 'DC 6', 'FORM 5.3']);
  });
});

describe('formChangeText', () => {
  it('signs rises, keeps the minus, and dashes a missing change', () => {
    expect(formChangeText(2.3)).toBe('+2.3');
    expect(formChangeText(-1)).toBe('-1.0');
    expect(formChangeText(0)).toBe('0.0');
    expect(formChangeText(null)).toBe('–');
  });
});

describe('RowAction', () => {
  it('names each action and shows the waiver run day in the W badge', async () => {
    const onPress = jest.fn();
    await render(
      <>
        <RowAction kind="add" name="C. Strand" onPress={onPress} />
        <RowAction kind="claim" day="Tue" name="A. Crispin" onPress={onPress} />
        <RowAction kind="trade" name="H. Szabo" onPress={onPress} />
      </>,
    );
    expect(screen.getByLabelText('Add C. Strand')).toBeTruthy();
    expect(screen.getByLabelText('Claim A. Crispin, waivers run Tue')).toBeTruthy();
    expect(screen.getByTestId('waiver-day')).toHaveTextContent('(Tue)');
    await fireEvent.press(screen.getByLabelText('Trade for H. Szabo'));
    expect(onPress).toHaveBeenCalledTimes(1);
  });
});
