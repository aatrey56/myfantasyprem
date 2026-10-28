import { useRef, useState, type Ref } from 'react';
import { Pressable, ScrollView, View } from 'react-native';

import { AppText } from '@/components/AppText';
import { Check } from '@/components/icons';
import { pressFeedback, type PressState } from '@/components/pressable';
import { Sheet, type SheetHandle } from '@/components/Sheet';
import { TextField } from '@/components/TextField';
import { filterSortGroups, type PlayerSort } from '@/lib/playerSort';
import { tint, useTheme } from '@/theme';

/**
 * The Players sort dialog: a search field and the sorts in groups (compact
 * rows, scrolling inside a capped height). A pick applies at once and closes
 * the dialog. Every sort is highest first; there is no order control.
 */
export function SortSheet({
  visible,
  sort,
  onClose,
  onPick,
}: {
  visible: boolean;
  sort: PlayerSort;
  onClose: () => void;
  onPick: (sort: PlayerSort) => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const handle = useRef<SheetHandle>(null);
  const [text, setText] = useState('');
  const groups = filterSortGroups(text);
  return (
    <Sheet
      ref={handle as Ref<SheetHandle>}
      visible={visible}
      // A fresh search each time it opens.
      onClose={() => {
        setText('');
        onClose();
      }}
      title="Sort by"
      testID="sort-sheet"
      presentation="center"
    >
      <TextField
        label="Search stats"
        value={text}
        onChangeText={setText}
        placeholder="xG, clean sheets, tackles"
        autoCorrect={false}
        autoCapitalize="none"
        testID="sort-search"
      />
      <ScrollView
        nestedScrollEnabled
        keyboardShouldPersistTaps="handled"
        style={{ maxHeight: 360 }}
        testID="sort-options"
      >
        {groups.length === 0 ? (
          <AppText variant="meta" muted style={{ paddingVertical: spacing.md }}>
            No stats match
          </AppText>
        ) : null}
        {groups.map((g) => (
          <View key={g.title} role="group" accessibilityLabel={g.title}>
            <AppText
              variant="eyebrow"
              muted
              style={{ paddingTop: spacing.md, paddingBottom: spacing.xs }}
            >
              {g.title}
            </AppText>
            {g.options.map((x) => {
              const selected = sort === x.value;
              return (
                <Pressable
                  key={x.value}
                  role="button"
                  accessibilityLabel={x.label}
                  accessibilityState={{ selected }}
                  aria-pressed={selected}
                  onPress={() => {
                    onPick(x.value);
                    handle.current?.closeAnimated();
                  }}
                  testID={`sort-${x.value}`}
                  style={(state: PressState) => [
                    {
                      minHeight: 36,
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
                  {x.short !== x.label ? (
                    <AppText variant="caption" subtle>
                      {x.short}
                    </AppText>
                  ) : null}
                  {selected ? <Check size={14} color={colors.accentBright} aria-hidden /> : null}
                </Pressable>
              );
            })}
          </View>
        ))}
      </ScrollView>
    </Sheet>
  );
}
