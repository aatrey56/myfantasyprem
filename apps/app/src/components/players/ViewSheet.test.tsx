/**
 * The View dialog's All players and positions need no request: they show
 * whatever the club list does. Only the club section waits or fails.
 */
import { fireEvent, render, screen } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { ViewSheet } from './ViewSheet';

// The dialog's frame and the skeleton's shimmer animate forever under Jest: draw just the content.
jest.mock('@/components/Sheet', () => ({
  Sheet: ({ children }: { children: React.ReactNode }) => children,
}));
jest.mock('@/components/Skeleton', () => ({ SkeletonRows: () => null }));

let mockClubs: Record<string, unknown>;
jest.mock('@/api/hooks', () => ({
  ...jest.requireActual('@/api/hooks'),
  useClubs: () => mockClubs,
}));

const failed = {
  data: undefined,
  error: new Error('Network down'),
  isError: true,
  isLoadingError: true,
  isPending: false,
  isLoading: false,
  isFetching: false,
  isRefetchError: false,
  errorUpdatedAt: 1,
  refetch: jest.fn(),
};
const loading = {
  ...failed,
  error: null,
  isError: false,
  isLoadingError: false,
  isPending: true,
  isLoading: true,
  isFetching: true,
  errorUpdatedAt: 0,
};

const open = async (onPick = jest.fn()) => {
  await render(
    <SafeAreaProvider
      initialMetrics={{
        frame: { x: 0, y: 0, width: 390, height: 844 },
        insets: { top: 0, left: 0, right: 0, bottom: 0 },
      }}
    >
      <ViewSheet visible view={{ kind: 'all' }} onClose={jest.fn()} onPick={onPick} />
    </SafeAreaProvider>,
  );
  return onPick;
};

const FIXED = ['view-all', 'view-pos-GKP', 'view-pos-DEF', 'view-pos-MID', 'view-pos-FWD'];

describe('ViewSheet', () => {
  it('a failed club list keeps All players and the positions; only clubs show the error', async () => {
    mockClubs = failed;
    const onPick = await open();
    for (const id of FIXED) expect(screen.getByTestId(id)).toBeTruthy();
    expect(screen.queryAllByTestId(/^view-club-/)).toHaveLength(0);
    expect(screen.getByTestId('query-error')).toBeTruthy();
    await fireEvent.press(screen.getByTestId('view-pos-DEF'));
    expect(onPick).toHaveBeenCalledWith({ kind: 'position', position: 'DEF' });
  });

  it('while clubs load, All players and the positions can be picked', async () => {
    mockClubs = loading;
    await open();
    for (const id of FIXED) expect(screen.getByTestId(id)).toBeTruthy();
    expect(screen.queryByTestId('query-error')).toBeNull();
  });
});
