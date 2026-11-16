import { View } from 'react-native';

import { AppText } from './AppText';
import { useOwnerLabelMode } from '@/hooks/useOwnerLabelMode';
import { ownerChipColors, ownerChipLabel, type OwnerTeam } from '@/lib/owner-label';
import { useTheme } from '@/theme';

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
  team: OwnerTeam;
  mine: boolean;
  /** The full owner, read out in place of the shortened text. */
  a11y?: string;
  testID?: string;
}) {
  const { colors, radii } = useTheme();
  const [mode] = useOwnerLabelMode();
  const { text, color } = ownerChipLabel(team, mine, mode);
  const fill = ownerChipColors(color, mine, colors);
  return (
    <View
      testID={testID ?? (mine ? 'owner-badge-mine' : 'owner-badge')}
      accessibilityLabel={a11y ?? `${team.name}${mine ? ' (your team)' : ''}`}
      {...({ title: team.name } as object)}
      style={{
        // `You` never shrinks; a name gives way to an ellipsis.
        flexShrink: mine ? 0 : 1,
        minWidth: 0,
        maxWidth: OWNER_CHIP_MAX_WIDTH,
        height: 20,
        paddingHorizontal: 6,
        flexDirection: 'row',
        alignItems: 'center',
        borderRadius: radii.sm,
        borderWidth: 1,
        borderColor: fill.border,
        backgroundColor: fill.background,
      }}
    >
      <AppText
        variant="caption"
        weight="bold"
        numberOfLines={1}
        aria-hidden
        color={fill.text}
        style={{ flexShrink: 1 }}
      >
        {text}
      </AppText>
    </View>
  );
}
