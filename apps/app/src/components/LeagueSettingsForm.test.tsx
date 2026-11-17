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
