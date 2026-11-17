import AsyncStorage from '@react-native-async-storage/async-storage';
import { act, render, screen } from '@testing-library/react-native';

import { OWNER_CHIP_MAX_WIDTH, OwnerChip } from './OwnerChip';
import { resetOwnerLabelModeForTests, setOwnerLabelMode } from '@/hooks/useOwnerLabelMode';

const team = { name: 'Park The Bus FC', managerName: 'J Doe' };

beforeEach(async () => {
  await AsyncStorage.clear();
  resetOwnerLabelModeForTests();
});

describe('OwnerChip', () => {
  it('names the team by default and is read out by its full label', async () => {
    await render(<OwnerChip team={team} mine={false} a11y="Park The Bus FC in League One" />);
    const chip = screen.getByTestId('owner-badge');
    expect(chip).toHaveTextContent('Park The Bus FC');
    expect(chip.props.accessible).toBe(true);
    expect(chip.props.accessibilityLabel).toBe('Park The Bus FC in League One');
    expect(screen.getByLabelText('Park The Bus FC in League One')).toBe(chip);
  });

  it('follows "Show owners as", live', async () => {
    await render(<OwnerChip team={team} mine={false} />);
    const chip = screen.getByTestId('owner-badge');
    await act(async () => setOwnerLabelMode('manager_name'));
    expect(chip).toHaveTextContent('J Doe');
    await act(async () => setOwnerLabelMode('first_name'));
    expect(chip).toHaveTextContent('J Doe');
    await act(async () => setOwnerLabelMode('team_name'));
    expect(chip).toHaveTextContent('Park The Bus FC');
  });

  it('shows the whole manager name in manager mode, the first name in first-name mode', async () => {
    await render(
      <OwnerChip team={{ name: 'Sunday Heroes', managerName: 'Sam Rivers' }} mine={false} />,
    );
    const chip = screen.getByTestId('owner-badge');
    await act(async () => setOwnerLabelMode('manager_name'));
    expect(chip).toHaveTextContent('Sam Rivers');
    await act(async () => setOwnerLabelMode('first_name'));
    expect(chip).toHaveTextContent('Sam');
    expect(chip).not.toHaveTextContent('Rivers');
  });

  it('keeps one line within its maximum width, cut with an ellipsis', async () => {
    await render(
      <OwnerChip
        team={{
          name: 'A Very Long Team Name Indeed',
          managerName: 'Maximilian Featherstonehaugh-Cholmondeley',
        }}
        mine={false}
      />,
    );
    await act(async () => setOwnerLabelMode('manager_name'));
    expect(screen.getByTestId('owner-badge')).toHaveStyle({ maxWidth: OWNER_CHIP_MAX_WIDTH });
    expect(
      screen.getByText('Maximilian Featherstonehaugh-Cholmondeley', { includeHiddenElements: true })
        .props.numberOfLines,
    ).toBe(1);
  });

  it('labels my own chip as my team, You in every mode', async () => {
    await render(<OwnerChip team={{ name: 'Mine FC', managerName: 'Sam' }} mine />);
    const chip = screen.getByTestId('owner-badge-mine');
    expect(chip.props.accessible).toBe(true);
    expect(chip.props.accessibilityLabel).toBe('Mine FC (your team)');
    for (const mode of ['team_name', 'manager_name', 'first_name'] as const) {
      await act(async () => setOwnerLabelMode(mode));
      expect(chip).toHaveTextContent('You');
    }
  });
});
