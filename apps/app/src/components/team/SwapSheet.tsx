import type { Schemas } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { LineupRow } from '../RosterRow';
import { Sheet, SheetCloseButton } from '../Sheet';
import type { PlayerScore } from './LineupViews';
import { checkSwap, linesOf, type LineupState } from '@/lib/lineup';
import { useTheme } from '@/theme';

type Entry = Schemas['LineupEntry'];

/**
 * The players `selectedId` can legally swap with, by the same rules a tap
 * on the pitch checks (`checkSwap`): bench players for a starter; starters
 * and other bench places for a bench player. Starters are in line order.
 */
export function swapCandidates(
  state: LineupState,
  selectedId: string,
  rules: Schemas['LineupRules'],
): { starters: Entry[]; bench: Entry[] } {
  const legal = (e: Entry) => checkSwap(state, selectedId, e.player.id, rules).kind === 'swap';
  return {
    starters: linesOf(state.starters)
      .flatMap((l) => l.entries)
      .filter(legal),
    bench: state.bench.filter(legal),
  };
}

interface Props {
  state: LineupState;
  /**
   * The player whose pill was tapped; null closes the sheet. A player with
   * no legal swap (or not in the lineup) keeps it closed.
   */
  selectedId: string | null;
  rules: Schemas['LineupRules'];
  scores: ReadonlyMap<string, PlayerScore> | null;
  /** Swap the selected player with this one. */
  onSwap: (playerId: string) => void;
  /** Cancel: nothing changes. */
  onClose: () => void;
}

/**
 * "Swap player": the selected player at the top, a divider chip, then only
 * the players they can legally swap with. Tapping one swaps; closing (the
 * ✕, the backdrop, Escape or a swipe down) leaves the lineup as it was.
 */
export function SwapSheet({ state, selectedId, rules, scores, onSwap, onClose }: Props) {
  const { colors, spacing, radii, fontFamilies } = useTheme();
  // Keep drawing the last player while the sheet slides away.
  const [shownId, setShownId] = useState(selectedId);
  if (selectedId !== null && selectedId !== shownId) setShownId(selectedId);
  const id = selectedId ?? shownId;
  const benchIndex = state.bench.findIndex((e) => e.player.id === id);
  const entry =
    state.starters.find((e) => e.player.id === id) ??
    (benchIndex >= 0 ? state.bench[benchIndex] : undefined);
  const candidates = id && entry ? swapCandidates(state, id, rules) : { starters: [], bench: [] };
  const count = candidates.starters.length + candidates.bench.length;
  // Never a sheet with nothing to choose: the opener says so instead.
  const visible = selectedId !== null && entry !== undefined && count > 0;

  const row = (e: Entry, bench: boolean) => {
    const score = scores?.get(e.player.id);
    return (
      <LineupRow
        key={e.player.id}
        testID={`swap-candidate-${e.player.id}`}
        player={e.player}
        fixtures={e.fixtures}
        points={score ? score.points : null}
        autoSub={score?.autoSub}
        bench={bench}
        place={bench ? `bench ${state.bench.indexOf(e) + 1}` : 'starting'}
        pressLabel={`Swap with ${e.player.displayName}`}
        onPress={() => onSwap(e.player.id)}
      />
    );
  };

  const section = (title: string, entries: Entry[], bench: boolean) =>
    entries.length > 0 ? (
      <View key={title} style={{ gap: spacing.xs }}>
        <AppText weight="bold" accessibilityRole="header">
          {title}
        </AppText>
        <View>{entries.map((e) => row(e, bench))}</View>
      </View>
    ) : null;

  return (
    <Sheet
      visible={visible}
      onClose={onClose}
      title="Swap Player"
      testID="swap-sheet"
      hideHeader
      presentation="center"
    >
      {/* Sleeper's centred bold title; the ✕ stays for the keyboard and screen readers. */}
      <View style={{ justifyContent: 'center', minHeight: 44 }}>
        <AppText
          accessibilityRole="header"
          testID="swap-title"
          style={{
            fontFamily: fontFamilies.bold,
            fontSize: 18,
            lineHeight: 24,
            textAlign: 'center',
            paddingHorizontal: 44,
          }}
        >
          Swap Player
        </AppText>
        <View style={{ position: 'absolute', right: -spacing.sm, top: 0 }}>
          <SheetCloseButton />
        </View>
      </View>
      {entry ? (
        <>
          <LineupRow
            testID="swap-selected"
            player={entry.player}
            fixtures={entry.fixtures}
            points={scores?.get(entry.player.id)?.points ?? null}
            bench={benchIndex >= 0}
            place={benchIndex >= 0 ? `bench ${benchIndex + 1}` : 'starting'}
          />
          <View
            style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}
            accessibilityRole="text"
          >
            <View style={{ flex: 1, height: 1, backgroundColor: colors.border }} />
            <View
              style={{
                paddingHorizontal: spacing.md,
                paddingVertical: spacing.xs,
                borderRadius: radii.pill,
                borderWidth: 1,
                borderColor: colors.border,
                backgroundColor: colors.surface,
              }}
            >
              <AppText variant="meta" weight="semibold" muted testID="swap-prompt">
                ⇅ Choose a player to swap
              </AppText>
            </View>
            <View style={{ flex: 1, height: 1, backgroundColor: colors.border }} />
          </View>
          {section('Starters', candidates.starters, false)}
          {section('Bench', candidates.bench, true)}
        </>
      ) : null}
    </Sheet>
  );
}
