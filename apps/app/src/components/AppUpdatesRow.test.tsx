import { fireEvent, render, screen } from '@testing-library/react-native';

import { AppUpdatesRow, type UpdatesApi } from './AppUpdatesRow';

const mockToast = jest.fn();
jest.mock('./Toast', () => ({ useToast: () => mockToast }));

function fake(over: Partial<UpdatesApi> = {}): UpdatesApi {
  return {
    isEnabled: true,
    updateId: '0b5c3d2e-1f4a-4b6c-8d7e-9fa0b1c2d3e4',
    createdAt: new Date(Date.now() - 5 * 60_000),
    isEmbeddedLaunch: false,
    runtimeVersion: 'c7cb618a5665315d',
    checkForUpdateAsync: jest.fn(async () => ({ isAvailable: false, isRollBackToEmbedded: false })),
    fetchUpdateAsync: jest.fn(async () => ({ isNew: true, isRollBackToEmbedded: false })),
    reloadAsync: jest.fn(async () => undefined),
    ...over,
  } as UpdatesApi;
}

const renderRow = async (updates: UpdatesApi) => render(<AppUpdatesRow updates={updates} />);

describe('AppUpdatesRow', () => {
  it('renders nothing where updates are off (web, tests, builds without APP_UPDATES_URL)', async () => {
    await renderRow(fake({ isEnabled: false }));
    expect(screen.queryByTestId('app-updates')).toBeNull();
  });

  it('shows the running update and the runtime', async () => {
    await renderRow(fake());
    expect(screen.getByText('Update 0b5c3d2e, published 5m ago · runtime c7cb618a')).toBeTruthy();
  });

  it('says when the built-in bundle runs', async () => {
    await renderRow(fake({ isEmbeddedLaunch: true, updateId: null }));
    expect(screen.getByText(/^Built-in version/)).toBeTruthy();
  });

  it('checks, downloads and offers a restart', async () => {
    const updates = fake({
      checkForUpdateAsync: jest.fn(async () => ({
        isAvailable: true as const,
        manifest: {} as never,
        isRollBackToEmbedded: false as const,
        reason: undefined,
      })),
    });
    await renderRow(updates);
    await fireEvent.press(screen.getByTestId('app-updates-check'));
    expect(updates.fetchUpdateAsync).toHaveBeenCalled();
    await fireEvent.press(screen.getByTestId('app-updates-restart'));
    expect(updates.reloadAsync).toHaveBeenCalled();
  });

  it('stays put when already up to date', async () => {
    const updates = fake();
    await renderRow(updates);
    await fireEvent.press(screen.getByTestId('app-updates-check'));
    expect(updates.fetchUpdateAsync).not.toHaveBeenCalled();
    expect(mockToast).toHaveBeenCalledWith({
      message: 'You have the latest version',
      tone: 'success',
    });
    expect(screen.queryByTestId('app-updates-restart')).toBeNull();
  });
});
