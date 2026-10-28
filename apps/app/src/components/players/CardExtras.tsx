import type { Schemas } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { Button } from '../Button';
import { Chip } from '../Chip';
import { ArrowLeftRight, Newspaper } from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import { TextField } from '../TextField';
import { useToast } from '../Toast';
import { useSetTag, useTagByPlayer } from '@/api/tag-hooks';
import { useBlockByPlayer, useSetTradeBlock } from '@/api/trade-block-hooks';
import { describeError } from '@/lib/errors';
import { TAGS } from '@/lib/tags';
import { formatAgo } from '@/lib/time';
import { BLOCK_POSITIONS, MAX_NOTE, wantedText } from '@/lib/trade-block';
import { tint, useTheme } from '@/theme';

type Player = Schemas['PlayerRef'];

/** `· 2h ago` after a news line, when the source gave a time. */
export function newsTime(newsAt: string | null | undefined, now = Date.now()): string {
  if (!newsAt) return '';
  const ms = Date.parse(newsAt);
  return Number.isFinite(ms) ? formatAgo(ms, now) : '';
}

/**
 * The news line on the card for an available player with news ("Returned
 * to training · 3h ago"); a flagged player's news is in the status banner.
 */
export function NewsLine({ player }: { player: Player }) {
  const { colors, spacing } = useTheme();
  const news = player.news?.trim();
  if (!news || player.availability !== 'available') return null;
  const at = newsTime(player.newsAt);
  return (
    <View
      testID="card-news"
      accessible
      accessibilityLabel={`News: ${news}${at ? `, ${at}` : ''}`}
      style={{ flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm }}
    >
      <Newspaper size={15} color={colors.textMuted} aria-hidden />
      <AppText variant="meta" muted style={{ flex: 1 }}>
        {news}
        {at ? <AppText variant="meta" subtle testID="card-news-time">{` · ${at}`}</AppText> : null}
      </AppText>
    </View>
  );
}

/**
 * The card's "Trade block" toggle for my own player: puts him on my block
 * (or takes him off) at once; the panel below edits the note and wants.
 */
export function BlockToggle({
  leagueId,
  player,
  team,
}: {
  leagueId: string;
  player: Player;
  team: Schemas['TeamRef'];
}) {
  const { radii } = useTheme();
  const toast = useToast();
  const entry = useBlockByPlayer(leagueId).get(player.id);
  const set = useSetTradeBlock(leagueId);
  const on = Boolean(entry);
  return (
    <Button
      label={on ? 'On the block' : 'Trade block'}
      icon={ArrowLeftRight}
      variant={on ? 'primary' : 'secondary'}
      style={{ borderRadius: radii.pill }}
      accessibilityLabel={
        on
          ? `Take ${player.displayName} off your trade block`
          : `Put ${player.displayName} on your trade block`
      }
      onPress={() =>
        set.mutate(on ? { player, on: false } : { player, team, on: true }, {
          onSuccess: () =>
            toast({
              message: on
                ? `${player.displayName} is off your trade block`
                : `${player.displayName} is on your trade block`,
              tone: 'success',
              haptic: false,
            }),
          onError: (e) => toast({ message: describeError(e), tone: 'error', haptic: false }),
        })
      }
      testID="card-block"
    />
  );
}

/**
 * The block panel: for my player on the block, a short note and the
 * positions I want back, saved together; for another team's, what they
 * want for him.
 */
export function BlockPanel({
  leagueId,
  player,
  mine,
}: {
  leagueId: string;
  player: Player;
  mine: boolean;
}) {
  const entry = useBlockByPlayer(leagueId).get(player.id);
  if (!entry) return null;
  return mine ? (
    <BlockEditor key={`${entry.note}|${entry.wanted.join()}`} leagueId={leagueId} entry={entry} />
  ) : (
    <BlockInfo entry={entry} />
  );
}

function BlockInfo({ entry }: { entry: Schemas['TradeBlockEntry'] }) {
  const { colors, spacing, radii } = useTheme();
  return (
    <View
      testID="card-block-info"
      accessible
      accessibilityLabel={`On ${entry.team.name}'s trade block. ${wantedText(entry.wanted)}${entry.note ? `. ${entry.note}` : ''}`}
      style={{
        gap: 2,
        padding: spacing.md,
        borderRadius: radii.md,
        backgroundColor: tint(colors.accent, colors.surface),
      }}
    >
      <AppText variant="meta" weight="bold" color={colors.accentBright}>
        {`ON THE TRADE BLOCK · ${wantedText(entry.wanted)}`}
      </AppText>
      {entry.note ? (
        <AppText variant="meta" muted>
          {`“${entry.note}”`}
        </AppText>
      ) : null}
    </View>
  );
}

function BlockEditor({ leagueId, entry }: { leagueId: string; entry: Schemas['TradeBlockEntry'] }) {
  const { colors, spacing, radii } = useTheme();
  const toast = useToast();
  const set = useSetTradeBlock(leagueId);
  const [note, setNote] = useState(entry.note);
  const [wanted, setWanted] = useState<Schemas['Position'][]>(entry.wanted);
  const changed = note.trim() !== entry.note || wanted.join() !== entry.wanted.join();
  const toggle = (p: Schemas['Position']) =>
    setWanted((w) =>
      w.includes(p)
        ? w.filter((x) => x !== p)
        : BLOCK_POSITIONS.filter((x) => x === p || w.includes(x)),
    );
  return (
    <View
      testID="card-block-editor"
      style={{
        gap: spacing.sm,
        padding: spacing.md,
        borderRadius: radii.md,
        borderWidth: 1,
        borderColor: colors.border,
      }}
    >
      <AppText variant="eyebrow" muted>
        On your trade block
      </AppText>
      <TextField
        label="Note (optional)"
        value={note}
        onChangeText={setNote}
        maxLength={MAX_NOTE}
        placeholder="Open to offers"
        hint={`${note.length}/${MAX_NOTE}`}
        testID="card-block-note"
      />
      <AppText variant="meta" muted>
        Looking for
      </AppText>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs }}>
        {BLOCK_POSITIONS.map((p) => (
          <Chip
            key={p}
            label={p}
            selected={wanted.includes(p)}
            onPress={() => toggle(p)}
            testID={`card-block-want-${p}`}
            accessibilityLabel={`Want a ${p} back`}
          />
        ))}
      </View>
      <Button
        label="Save"
        variant="secondary"
        disabled={!changed}
        onPress={() =>
          set.mutate(
            { player: entry.player, team: entry.team, on: true, note: note.trim(), wanted },
            {
              onSuccess: () =>
                toast({ message: 'Trade block saved', tone: 'success', haptic: false }),
              onError: (e) => toast({ message: describeError(e), tone: 'error', haptic: false }),
            },
          )
        }
        testID="card-block-save"
      />
    </View>
  );
}

/** What tags are for, under the tag picker's label. */
export const TAG_HELP =
  'Private labels only you can see. Tag players to remember your plans, then filter the Players list by tag.';

/**
 * My tag on the player: one of the fixed set, private to me (Sleeper's
 * personal tags). Tapping the chosen tag again clears it.
 */
export function TagPicker({ leagueId, player }: { leagueId: string; player: Player }) {
  const { colors, spacing, radii } = useTheme();
  const toast = useToast();
  const current = useTagByPlayer(leagueId).get(player.id);
  const set = useSetTag(leagueId);
  return (
    <View style={{ gap: spacing.xs }} testID="card-tags">
      <AppText variant="eyebrow" muted>
        My tag
      </AppText>
      <AppText variant="meta" subtle testID="card-tags-help">
        {TAG_HELP}
      </AppText>
      <View role="radiogroup" style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs }}>
        {TAGS.map((t) => {
          const selected = current?.tag === t.tag;
          return (
            <Pressable
              key={t.tag}
              role="radio"
              accessibilityLabel={`Tag ${t.label}`}
              accessibilityState={{ selected }}
              aria-checked={selected}
              onPress={() =>
                set.mutate(
                  { player, tag: selected ? null : t.tag },
                  {
                    onError: (e) =>
                      toast({ message: describeError(e), tone: 'error', haptic: false }),
                  },
                )
              }
              hitSlop={4}
              testID={`card-tag-${t.tag}`}
              style={(s: PressState) => [
                {
                  flexDirection: 'row',
                  alignItems: 'center',
                  gap: 4,
                  minHeight: 36,
                  paddingHorizontal: spacing.sm,
                  borderRadius: radii.pill,
                  borderWidth: 1.5,
                  borderColor: selected ? colors.accent : colors.border,
                  backgroundColor: selected
                    ? tint(colors.accent, colors.surface)
                    : colors.surfaceControl,
                },
                ...pressFeedback(colors, s),
              ]}
            >
              <AppText style={{ fontSize: 15, lineHeight: 18 }}>{t.emoji}</AppText>
              <AppText
                variant="meta"
                weight="semibold"
                color={selected ? colors.accentBright : colors.textMuted}
              >
                {t.label}
              </AppText>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}
