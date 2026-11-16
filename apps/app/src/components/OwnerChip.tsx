import { View } from 'react-native';

import { AppText } from './AppText';
import { useOwnerLabelMode } from '@/hooks/useOwnerLabelMode';
import { ownerChipLabel } from '@/lib/owner-label';
import { tint, useTheme } from '@/theme';

/** The widest a chip gets; a longer name ends in an ellipsis. */
export const OWNER_CHIP_MAX_WIDTH = 96;

/**
 * Who owns a player: the manager's first name (or team name, per "Show
 * owners as" in Account) on the team's avatar colour; `You` in solid
 * accent on my own team. One line, never wider than OWNER_CHIP_MAX_WIDTH.
 */
export function OwnerChip({
  team,
  mine,
  a11y,
  testID,
}: {
  team: { name: string; managerName: string };
  mine: boolean;
  /** The full owner, read out in place of the shortened text. */
  a11y?: string;
  testID?: string;
}) {
  const { colors, radii } = useTheme();
  const [mode] = useOwnerLabelMode();
  const { text, color } = ownerChipLabel(team, mine, mode);
  return (
    <View
      testID={testID ?? (mine ? 'owner-badge-mine' : 'owner-badge')}
      accessibilityLabel={a11y ?? `${team.name}${mine ? ' (your team)' : ''}`}
      {...({ title: team.name } as object)}
      style={{
        flexShrink: 1,
        minWidth: 0,
        maxWidth: OWNER_CHIP_MAX_WIDTH,
        height: 20,
        paddingHorizontal: 6,
        flexDirection: 'row',
        alignItems: 'center',
        borderRadius: radii.sm,
        borderWidth: 1,
        borderColor: mine ? colors.accent : tint(color, colors.surface, 0.55),
        backgroundColor: mine ? colors.accent : tint(color, colors.surface, 0.18),
      }}
    >
      <AppText
        variant="caption"
        weight="bold"
        numberOfLines={1}
        aria-hidden
        color={mine ? colors.onAccent : colors.text}
        style={{ flexShrink: 1 }}
      >
        {text}
      </AppText>
    </View>
  );
}
