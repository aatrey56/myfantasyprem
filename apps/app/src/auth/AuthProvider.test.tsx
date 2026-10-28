import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react-native';
import { Text } from 'react-native';

import { AuthProvider, useAuth } from './AuthProvider';
import { memorySessionStore, tokens, type SessionStore, type StoredSession } from './session';

const user = (id: string) =>
  ({
    id,
    displayName: id,
    email: `${id}@example.com`,
    avatarColor: '#2f6fed',
    authProviders: ['email'],
    createdAt: '2026-07-01T09:00:00Z',
  }) as StoredSession['user'];

const session = (n: number, userId = 'usr_a'): StoredSession => ({
  accessToken: `a${n}`,
  refreshToken: `r${n}`,
  expiresAt: '2099-01-01T00:00:00Z',
  refreshExpiresAt: '2099-01-01T00:00:00Z',
  user: user(userId),
});

function Status() {
  const { status, user: u } = useAuth();
  return <Text testID="status">{`${status}:${u?.id ?? '-'}`}</Text>;
}

async function mount(store: SessionStore) {
  const queryClient = new QueryClient();
  await render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider store={store}>
        <Status />
      </AuthProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('signedIn:usr_a'));
  return queryClient;
}

beforeEach(() => tokens.set(null));

describe('AuthProvider', () => {
  it("drops the previous user's cached data when a failed refresh signs them out", async () => {
    const queryClient = await mount(memorySessionStore(session(1)));
    queryClient.setQueryData(['me', 'leagues'], { data: ['private league'] });
    // What the API client does when the refresh token is refused.
    await act(async () => tokens.set(null));
    expect(screen.getByTestId('status')).toHaveTextContent('signedOut:-');
    expect(queryClient.getQueryData(['me', 'leagues'])).toBeUndefined();
  });

  it('adopts a session another tab saved, and signs out when another tab does', async () => {
    let emit: (s: StoredSession | null) => void = () => undefined;
    const store: SessionStore = {
      ...memorySessionStore(session(1)),
      subscribe: (listener) => {
        emit = listener;
        return () => undefined;
      },
    };
    const queryClient = await mount(store);
    await act(async () => emit(session(2)));
    expect(tokens.get()?.refreshToken).toBe('r2');
    queryClient.setQueryData(['me', 'leagues'], { data: ['private league'] });
    await act(async () => emit(null));
    expect(tokens.get()).toBeNull();
    expect(screen.getByTestId('status')).toHaveTextContent('signedOut:-');
    expect(queryClient.getQueryData(['me', 'leagues'])).toBeUndefined();
  });

  it("persists this tab's changes but never writes another tab's session back", async () => {
    let emit: (s: StoredSession | null) => void = () => undefined;
    const base = memorySessionStore(session(1));
    const save = jest.fn(base.save);
    const store: SessionStore = {
      ...base,
      save,
      subscribe: (listener) => {
        emit = listener;
        return () => undefined;
      },
    };
    await mount(store);
    expect(save).not.toHaveBeenCalled(); // the launch load is not re-saved
    await act(async () => emit(session(2)));
    expect(save).not.toHaveBeenCalled(); // no storage-event echo
    await act(async () => tokens.set(session(3)));
    expect(save).toHaveBeenCalledWith(session(3));
    await expect(store.load()).resolves.toEqual(session(3));
    // A late event for a pair this tab already moved past is ignored.
    await act(async () => emit(session(2)));
    expect(tokens.get()?.refreshToken).toBe('r3');
  });
});
