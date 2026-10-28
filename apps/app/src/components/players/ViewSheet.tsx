import { useRef, type Ref } from 'react';
import { Pressable, ScrollView, View } from 'react-native';

import { useClubs } from '@/api/hooks';
import { AppText } from '@/components/AppText';
import { Check } from '@/components/icons';
import { pressFeedback, type PressState } from '@/components/pressable';
import { QueryStatus } from '@/components/QueryStatus';
import { Sheet, type SheetHandle } from '@/components/Sheet';
import { SkeletonRows } from '@/components/Skeleton';
import { viewId, viewOptions, type PlayerViewChoice, type ViewOption } from '@/lib/playerView';
import { tint, useTheme } from '@/theme';

const HEADINGS: Record<ViewOption['group'], string | null> = {
  all: null,
  position: 'By position',
  club: 'By club',
};

/**
 * The Players View dialog (FPL Draft's View dropdown): All players, the
 * four positions, then every club A–Z. One choice at a time; a pick
 * applies at once and closes the dialog.
 */
export function ViewSheet({
  visible,
  view,
  onClose,
  onPick,
}: {
  visible: boolean;
  view: PlayerViewChoice;
  onClose: () => void;
  onPick: (view: PlayerViewChoice) => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const handle = useRef<SheetHandle>(null);
  const clubs = useClubs();
  const current = viewId(view);
  const option = (x: ViewOption, i: number, options: ViewOption[]) => {
    const selected = x.id === current;
    const heading =
      x.group !== 'club' && options[i - 1]?.group !== x.group ? HEADINGS[x.group] : null;
    return (
      <View key={x.id}>
        {heading ? (
          <AppText
            variant="eyebrow"
            muted
            style={{ paddingTop: spacing.md, paddingBottom: spacing.xs }}
          >
            {heading}
          </AppText>
        ) : null}
        <Pressable
          role="radio"
          accessibilityLabel={x.label}
          accessibilityState={{ checked: selected }}
          aria-checked={selected}
          onPress={() => {
            onPick(x.choice);
            handle.current?.closeAnimated();
          }}
          testID={`view-${x.id}`}
          style={(state: PressState) => [
            {
              minHeight: 44,
              flexDirection: 'row',
              alignItems: 'center',
              gap: spacing.sm,
              paddingHorizontal: spacing.sm,
              borderRadius: radii.sm,
              backgroundColor: selected ? tint(colors.accent, colors.surface) : undefined,
            },
            ...pressFeedback(colors, state, { hoverFill: !selected }),
          ]}
        >
          <AppText
            variant="meta"
            weight={selected ? 'bold' : 'regular'}
            color={selected ? colors.accentBright : undefined}
            style={{ flex: 1 }}
            numberOfLines={1}
          >
            {x.label}
          </AppText>
          {selected ? <Check size={14} color={colors.accentBright} aria-hidden /> : null}
        </Pressable>
      </View>
    );
  };
  return (
    <Sheet
      ref={handle as Ref<SheetHandle>}
      visible={visible}
      onClose={onClose}
      title="View"
      testID="view-sheet"
      presentation="center"
    >
      <ScrollView
        nestedScrollEnabled
        style={{ maxHeight: 420 }}
        role="radiogroup"
        accessibilityLabel="View"
        testID="view-options"
      >
        {/* All players and the positions need no request: only the clubs wait or fail. */}
        {viewOptions([]).map(option)}
        <AppText
          variant="eyebrow"
          muted
          style={{ paddingTop: spacing.md, paddingBottom: spacing.xs }}
        >
          {HEADINGS.club}
        </AppText>
        <QueryStatus query={clubs} loading={<SkeletonRows count={3} />}>
          {({ data }) => (
            <>
              {viewOptions(data)
                .filter((x) => x.group === 'club')
                .map(option)}
            </>
          )}
        </QueryStatus>
      </ScrollView>
    </Sheet>
  );
}
