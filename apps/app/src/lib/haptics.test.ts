import * as Haptics from 'expo-haptics';

jest.mock('expo-haptics', () => ({
  notificationAsync: jest.fn(() => Promise.resolve()),
  impactAsync: jest.fn(() => Promise.resolve()),
  selectionAsync: jest.fn(() => Promise.resolve()),
  NotificationFeedbackType: { Success: 'success', Warning: 'warning', Error: 'error' },
  ImpactFeedbackStyle: { Light: 'light', Medium: 'medium', Heavy: 'heavy' },
}));

/** Loads the module fresh for a platform (it reads Platform.OS once). */
function load(os: 'ios' | 'android' | 'web') {
  let mod!: typeof import('./haptics');
  jest.isolateModules(() => {
    jest.doMock('react-native', () => ({ Platform: { OS: os } }));
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    mod = require('./haptics') as typeof import('./haptics');
  });
  return mod.haptics;
}

beforeEach(() => jest.clearAllMocks());

describe('haptics', () => {
  it('a select or swap is a light impact', () => {
    load('ios').light();
    expect(Haptics.impactAsync).toHaveBeenCalledWith('light');
  });

  it('a confirmed move is a success notification, a rollback an error one', () => {
    const h = load('ios');
    h.success();
    h.error();
    expect(Haptics.notificationAsync).toHaveBeenNthCalledWith(1, 'success');
    expect(Haptics.notificationAsync).toHaveBeenNthCalledWith(2, 'error');
  });

  it('works on Android too', () => {
    load('android').light();
    expect(Haptics.impactAsync).toHaveBeenCalledTimes(1);
  });

  it('does nothing on web', () => {
    const h = load('web');
    h.light();
    h.success();
    h.error();
    h.selection();
    expect(Haptics.impactAsync).not.toHaveBeenCalled();
    expect(Haptics.notificationAsync).not.toHaveBeenCalled();
    expect(Haptics.selectionAsync).not.toHaveBeenCalled();
  });

  it('a device that refuses haptics does not throw', async () => {
    (Haptics.impactAsync as jest.Mock).mockReturnValueOnce(Promise.reject(new Error('off')));
    expect(() => load('ios').light()).not.toThrow();
    await Promise.resolve();
  });
});
