import { fireEvent, render, screen } from '@testing-library/react-native';

import { DEFAULT_SETTINGS, LeagueSettingsForm } from './LeagueSettingsForm';

describe('LeagueSettingsForm after the draft', () => {
  it('keeps the trade review control enabled, with a hint about trades in review', async () => {
    const onChange = jest.fn();
    await render(
      <LeagueSettingsForm
        value={{ ...DEFAULT_SETTINGS, review: 'commissioner' }}
        onChange={onChange}
        locked
      />,
    );
    expect(screen.getByTestId('trade-review')).toBeTruthy();
    expect(screen.queryByTestId('team-count')).toBeNull();
    expect(screen.queryByTestId('pick-timer')).toBeNull();
    expect(screen.getByTestId('settings-locked')).toHaveTextContent(
      /team count and the pick clock are locked.*trade review.*can still change/,
    );
    expect(screen.getByTestId('trade-review-hint')).toHaveTextContent(
      /Trades already in review keep the review they started with/,
    );
    await fireEvent.press(screen.getByRole('radio', { name: 'Vote' }));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ review: 'league_vote' }));
  });

  it('shows no control and no hint to a member reading the settings', async () => {
    await render(
      <LeagueSettingsForm value={DEFAULT_SETTINGS} onChange={() => undefined} locked readOnly />,
    );
    expect(screen.queryByTestId('trade-review')).toBeNull();
    expect(screen.queryByTestId('trade-review-hint')).toBeNull();
  });

  it('has no hint before the draft', async () => {
    await render(<LeagueSettingsForm value={DEFAULT_SETTINGS} onChange={() => undefined} />);
    expect(screen.getByTestId('trade-review')).toBeTruthy();
    expect(screen.queryByTestId('trade-review-hint')).toBeNull();
  });
});

describe('LeagueSettingsForm veto threshold', () => {
  const vote = { ...DEFAULT_SETTINGS, review: 'league_vote' as const, teamCount: 12 };

  it('counts the vetoes from the managers in the league, as the server does', async () => {
    // 12 seats, 9 managers: a third of the 7 outside a trade is 3 (of a full 12 it would be 4).
    await render(<LeagueSettingsForm value={vote} onChange={() => undefined} managers={9} />);
    expect(screen.getByTestId('veto-threshold-help')).toHaveTextContent(
      'With 9 managers, 3 of the 7 outside a trade must veto it to block it.',
    );
  });

  it('shows the same count to a member reading the settings', async () => {
    await render(
      <LeagueSettingsForm value={vote} onChange={() => undefined} locked readOnly managers={9} />,
    );
    expect(screen.getByTestId('review-summary')).toHaveTextContent(
      '24 hour review. With 9 managers, 3 of the 7 outside a trade must veto it to block it.',
    );
  });

  it('is for a full league before the league exists', async () => {
    await render(<LeagueSettingsForm value={vote} onChange={() => undefined} />);
    expect(screen.getByTestId('veto-threshold-help')).toHaveTextContent(
      'In a full league of 12, 4 of the 10 managers outside a trade must veto it to block it.',
    );
  });

  it('selects a preset the server sent through a float32', async () => {
    await render(
      <LeagueSettingsForm
        value={{ ...vote, vetoThreshold: Math.fround(1 / 3) }}
        onChange={() => undefined}
        managers={12}
      />,
    );
    expect(screen.getByRole('radio', { name: 'A third' })).toBeChecked();
    expect(screen.queryByRole('radio', { name: /%$/ })).toBeNull();
  });

  it('shows a custom threshold as it is stored, not as the nearest preset', async () => {
    const onChange = jest.fn();
    // 60%: two thirds is the nearest preset, which would need 7 of 10, not 6.
    await render(
      <LeagueSettingsForm
        value={{ ...vote, vetoThreshold: Math.fround(0.6) }}
        onChange={onChange}
        managers={12}
      />,
    );
    expect(screen.getByRole('radio', { name: '60%' })).toBeChecked();
    expect(screen.getByRole('radio', { name: 'Two thirds' })).not.toBeChecked();
    expect(screen.getByTestId('veto-threshold-help')).toHaveTextContent(
      'With 12 managers, 6 of the 10 outside a trade must veto it to block it.',
    );
    await fireEvent.press(screen.getByRole('radio', { name: 'Half' }));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ vetoThreshold: 0.5 }));
  });
});
