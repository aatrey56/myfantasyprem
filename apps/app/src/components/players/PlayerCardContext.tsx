import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { Pressable, type StyleProp, type TextStyle, type ViewStyle } from 'react-native';

import { AppText, type TextVariant } from '../AppText';
import { pressFeedback, type PressState } from '../pressable';
import { PlayerSheet } from './PlayerCard';
import { useTheme } from '@/theme';

/** Opens the player card; `drop` opens it on the Sign step (choose whom to drop). */
export type OpenPlayerCard = (playerId: string, options?: { drop?: boolean }) => void;

export const OpenCardContext = createContext<OpenPlayerCard | null>(null);

/**
 * One player card sheet for a league: every screen and list under it opens
 * cards through `usePlayerCard`, so any player shown anywhere is one tap from
 * their card.
 */
export function PlayerCardProvider({
  leagueId,
  children,
}: {
  leagueId: string;
  children: ReactNode;
}) {
  const [card, setCard] = useState<{ id: string; drop: boolean } | null>(null);
  const open = useCallback<OpenPlayerCard>(
    (id, options) => setCard({ id, drop: Boolean(options?.drop) }),
    [],
  );
  return (
    <OpenCardContext.Provider value={open}>
      {children}
      <PlayerSheet
        leagueId={leagueId}
        playerId={card?.id ?? null}
        startWithDrop={card?.drop}
        onClose={() => setCard(null)}
      />
    </OpenCardContext.Provider>
  );
}

/** Opens the league's player card, or null outside a league (nothing to open). */
export function usePlayerCard(): OpenPlayerCard | null {
  return useContext(OpenCardContext);
}

/** `Player card for X`: the label of every control that opens a player's card. */
export const playerCardLabel = (name: string) => `Player card for ${name}`;

/** A press handler that opens `playerId`'s card, or undefined outside a league. */
export function useOpenCardFor(playerId: string): (() => void) | undefined {
  const open = usePlayerCard();
  return useMemo(() => (open ? () => open(playerId) : undefined), [open, playerId]);
}

/**
 * A player's name as a button that opens their card (plain text outside a
 * league). For lists whose rows are not `PlayerRow`s: waiver claims, trades,
 * activity, the draft board and ticker. Actions stay separate buttons beside it.
 */
export function PlayerName({
  player,
  label,
  variant,
  weight,
  color,
  muted,
  numberOfLines = 1,
  textStyle,
  style,
  testID,
}: {
  player: { id: string; displayName: string };
  /** The text shown (default the display name), e.g. a shortened name. */
  label?: string;
  variant?: TextVariant;
  weight?: 'regular' | 'medium' | 'semibold' | 'bold';
  color?: string;
  muted?: boolean;
  numberOfLines?: number;
  textStyle?: StyleProp<TextStyle>;
  style?: StyleProp<ViewStyle>;
  testID?: string;
}) {
  const { colors, radii } = useTheme();
  const onPress = useOpenCardFor(player.id);
  const text = (
    <AppText
      variant={variant}
      weight={weight}
      color={color}
      muted={muted}
      numberOfLines={numberOfLines}
      style={textStyle}
    >
      {label ?? player.displayName}
    </AppText>
  );
  if (!onPress) return text;
  return (
    <Pressable
      testID={testID ?? `open-card-${player.id}`}
      accessibilityRole="button"
      accessibilityLabel={playerCardLabel(player.displayName)}
      onPress={onPress}
      hitSlop={6}
      style={(state: PressState) => [
        { flexShrink: 1, minWidth: 0, borderRadius: radii.sm },
        style,
        ...pressFeedback(colors, state),
      ]}
    >
      {text}
    </Pressable>
  );
}
