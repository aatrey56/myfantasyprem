import { render, screen } from '@testing-library/react-native';

import { OwnerChip } from './OwnerChip';

describe('OwnerChip', () => {
  it('is one accessible element read out by its full label', async () => {
    await render(
      <OwnerChip
        team={{ name: 'Park The Bus FC', managerName: 'J Doe' }}
        mine={false}
        a11y="Park The Bus FC in League One"
      />,
    );
    const chip = screen.getByTestId('owner-badge');
    expect(chip).toHaveTextContent('J Doe');
    expect(chip.props.accessible).toBe(true);
    expect(chip.props.accessibilityLabel).toBe('Park The Bus FC in League One');
    expect(screen.getByLabelText('Park The Bus FC in League One')).toBe(chip);
  });

  it('labels my own chip as my team', async () => {
    await render(<OwnerChip team={{ name: 'Mine FC', managerName: 'Sam' }} mine />);
    const chip = screen.getByTestId('owner-badge-mine');
    expect(chip.props.accessible).toBe(true);
    expect(chip.props.accessibilityLabel).toBe('Mine FC (your team)');
  });
});
