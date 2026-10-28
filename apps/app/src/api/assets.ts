import { send } from '@myfantasyprem/api-client';
import { useQuery } from '@tanstack/react-query';

import { api } from './client';
import { API_BASE_URL } from './config';

/**
 * Whether this build may show official club crests, shirts and player
 * photos. App Store and other public builds set
 * EXPO_PUBLIC_BRANDED_ASSETS=false (eas.json `production`); then nothing is
 * requested whatever the server says. Inline env check: Expo inlines it at
 * build time. Mock builds serve fixture images, native ones through
 * RemoteImage's in-process path (components/branded/mock-image.ts).
 */
export const BUILD_BRANDED_ASSETS = process.env.EXPO_PUBLIC_BRANDED_ASSETS !== 'false';

/** GET /v1/config: the server's client settings (its branded-assets switch). */
export function useAppConfig(enabled = BUILD_BRANDED_ASSETS) {
  return useQuery({
    queryKey: ['config'] as const,
    queryFn: () => send(api.GET('/v1/config')),
    staleTime: 60 * 60_000,
    enabled,
  });
}

// The app never loads these images from anywhere but our API, which
// fetches them upstream once and caches them (AGENTS.md hard rules).

export const playerPhotoUrl = (playerId: string) =>
  `${API_BASE_URL}/v1/assets/players/${encodeURIComponent(playerId)}/photo`;

export const clubCrestUrl = (clubCode: string) =>
  `${API_BASE_URL}/v1/assets/clubs/${encodeURIComponent(clubCode.toUpperCase())}/crest`;

export const clubShirtUrl = (clubCode: string, goalkeeper = false) =>
  `${API_BASE_URL}/v1/assets/clubs/${encodeURIComponent(clubCode.toUpperCase())}/shirt${goalkeeper ? '?gk=true' : ''}`;
