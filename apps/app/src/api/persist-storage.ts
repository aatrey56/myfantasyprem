import AsyncStorage from '@react-native-async-storage/async-storage';

import { flushablePersister, type FlushablePersister } from './persist-throttle';

/** Native: AsyncStorage. persist-storage.web.ts is the localStorage one. */
export function cacheStorage(key: string): FlushablePersister {
  return flushablePersister(AsyncStorage, key, 1_000);
}
