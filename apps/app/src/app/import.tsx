import { useRouter } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { TextInput, View } from 'react-native';

import { useCreateImport, useImport, useImportPreview, type LeagueRef } from '@/api/hooks';
import { parseLeagueInput } from '@/api/league-input';
import { AppText, INPUT_MAX_FONT_SCALE } from '@/components/AppText';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { ListRow } from '@/components/ListRow';
import { Screen } from '@/components/Screen';
import { useFinePointer } from '@/hooks/useFinePointer';
import { describeError } from '@/lib/errors';
import { importStatusText, previewFacts } from '@/lib/fpl-import';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

const CLASSIC_MESSAGE =
  'That is a classic FPL link (fantasy.premierleague.com). Draft and classic league ids overlap, so paste the link from draft.premierleague.com instead.';

const EXAMPLE_ID = '12345';
const EXAMPLE = `https://draft.premierleague.com/league/${EXAMPLE_ID}`;
/** The id part of the example, which is selected so typing replaces it. */
const EXAMPLE_ID_RANGE = { start: EXAMPLE.length - EXAMPLE_ID.length, end: EXAMPLE.length };

/**
 * Import an FPL Draft league already in its season (ADR 0012): paste the
 * league link or id (no FPL login), check "Is this your league?", pick your
 * own team, then follow the import job until the league opens.
 */
function ProgressBar({ done, total }: { done: number; total: number }) {
  const { colors, radii } = useTheme();
  const pct = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0;
  return (
    <View
      testID="import-progress"
      accessibilityRole="progressbar"
      accessibilityValue={{ min: 0, max: total, now: done }}
      style={{ height: 6, borderRadius: radii.pill, backgroundColor: colors.border }}
    >
      <View
        style={{
          width: `${pct}%`,
          height: 6,
          borderRadius: radii.pill,
          backgroundColor: colors.accent,
        }}
      />
    </View>
  );
}

export default function ImportLeagueScreen() {
  const router = useRouter();
  // Tab-to-fill needs a hardware keyboard: desktop web only, never touch or native.
  const canTab = useFinePointer();
  const { colors, radii, spacing, fontSizes } = useTheme();
  const [value, setValue] = useState('');
  const [selection, setSelection] = useState<{ start: number; end: number } | undefined>();
  const [inputError, setInputError] = useState<string | null>(null);
  const inputRef = useRef<TextInput>(null);
  const [ref, setRef] = useState<LeagueRef | null>(null);
  const preview = useImportPreview(ref);
  const createImport = useCreateImport();
  const job = useImport(createImport.data?.data.id, createImport.data?.retryAfterMs);
  const current = job.data?.data ?? createImport.data?.data;
  // Open enum: only queued and running mean "wait"; anything else lets the user retry.
  const active = current?.status === 'queued' || current?.status === 'running';
  const [seat, setSeat] = useState<number | null>(null);
  const busy = createImport.isPending || active;

  useEffect(() => {
    if (current?.status === 'succeeded' && current.leagueId) {
      router.replace(`/leagues/${current.leagueId}`);
    }
  }, [current?.status, current?.leagueId, router]);

  const find = () => {
    const parsed = parseLeagueInput(value);
    createImport.reset();
    if (parsed.kind === 'classic' || parsed.kind === 'invalid') {
      setRef(null);
      setInputError(parsed.kind === 'classic' ? CLASSIC_MESSAGE : parsed.reason);
      return;
    }
    setInputError(null);
    setSeat(null);
    // Always the normalised id: the server need not understand every link form.
    const next: LeagueRef = { fplLeagueId: parsed.fplLeagueId };
    // The same lookup again (say, after an error) must ask the server again.
    if (ref && JSON.stringify(ref) === JSON.stringify(next)) void preview.refetch();
    else setRef(next);
  };

  const fillExample = () => {
    setValue(EXAMPLE);
    setSelection(EXAMPLE_ID_RANGE);
    setInputError(null);
  };

  // Desktop web: Tab on the empty input fills the example and selects the id; with text,
  // Tab moves focus as usual. A native capture-phase keydown listener is used
  // because preventDefault on react-native-web's onKeyPress event does not
  // reliably stop the browser's focus move (Safari).
  useEffect(() => {
    if (!canTab) return;
    // On web the TextInput ref is the DOM <input>.
    const el = inputRef.current as unknown as HTMLInputElement | null;
    if (!el?.addEventListener) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab' || e.shiftKey || e.altKey || e.ctrlKey || e.metaKey) return;
      if (el.value !== '') return;
      e.preventDefault();
      e.stopPropagation();
      el.value = EXAMPLE;
      el.setSelectionRange(EXAMPLE_ID_RANGE.start, EXAMPLE_ID_RANGE.end);
      // Keep React state in sync with the DOM.
      setValue(EXAMPLE);
      setSelection(EXAMPLE_ID_RANGE);
      setInputError(null);
    };
    el.addEventListener('keydown', onKeyDown, true);
    return () => el.removeEventListener('keydown', onKeyDown, true);
  }, [canTab]);

  const errorText = (text: string, testID: string) => (
    <AppText testID={testID} style={{ color: colors.loss }}>
      {text}
    </AppText>
  );

  return (
    <Screen
      title="Import a league"
      eyebrow="FPL Draft"
      subtitle="Paste your FPL Draft league link or ID."
      back={{ label: 'Leagues', href: '/leagues' }}
    >
      <Card>
        <TextInput
          maxFontSizeMultiplier={INPUT_MAX_FONT_SCALE}
          testID="league-input"
          ref={inputRef}
          // Desktop web: focus on open so Tab fills the example straight away (single-purpose form).
          autoFocus={canTab}
          accessibilityLabel="FPL Draft league link or ID"
          value={value}
          onChangeText={(text) => {
            setValue(text);
            setSelection(undefined); // the programmatic selection has done its job
            setInputError(null);
          }}
          selection={selection}
          onSubmitEditing={find}
          autoCapitalize="none"
          autoCorrect={false}
          inputMode="url"
          placeholder="https://draft.premierleague.com/league/12345"
          placeholderTextColor={colors.textMuted}
          style={{
            color: colors.text,
            borderColor: colors.border,
            borderWidth: 1,
            borderRadius: radii.md,
            padding: spacing.md,
            fontSize: fontSizes.md,
          }}
        />
        {canTab ? (
          <AppText testID="example-hint" variant="caption" muted>
            Press Tab to use the example, then type your league id
          </AppText>
        ) : (
          <Button
            testID="use-example-button"
            variant="ghost"
            size="sm"
            label="Use example"
            onPress={fillExample}
          />
        )}
        <Button
          testID="find-button"
          label={preview.isFetching ? 'Looking it up…' : 'Find league'}
          disabled={!value.trim() || preview.isFetching}
          onPress={find}
        />
        {inputError ? errorText(inputError, 'input-error') : null}
        {preview.isError ? errorText(describeError(preview.error), 'preview-error') : null}
      </Card>

      {ref && preview.data ? (
        <Card testID="preview">
          <AppText variant="heading">Is this your league?</AppText>
          <AppText testID="preview-name" weight="semibold">
            {preview.data.data.name}
          </AppText>
          <AppText testID="preview-facts" muted>
            {previewFacts(preview.data.data)}
          </AppText>
          {preview.data.data.importable ? (
            <>
              <AppText variant="meta" muted>
                Which team is yours? You become the commissioner; everyone else gets a link to claim
                their team.
              </AppText>
              <View accessibilityRole="radiogroup" testID="seat-list">
                {preview.data.data.seats.map((s, i) => (
                  <ListRow
                    key={s.seat}
                    testID={`seat-option-${s.seat}`}
                    title={s.teamName}
                    subtitle={s.managerName}
                    divider={i > 0}
                    selected={seat === s.seat}
                    disabled={busy}
                    accessibilityLabel={`${s.teamName}, ${s.managerName}`}
                    onPress={() => setSeat(s.seat)}
                  />
                ))}
              </View>
            </>
          ) : (
            errorText(
              preview.data.data.problem?.detail ?? 'This league cannot be imported.',
              'preview-not-importable',
            )
          )}
          {current ? <AppText testID="import-status">{importStatusText(current)}</AppText> : null}
          {active ? (
            <ProgressBar done={current?.progress.done ?? 0} total={current?.progress.total ?? 0} />
          ) : null}
          {job.isError
            ? errorText(
                `Couldn't check the import: ${describeError(job.error)}`,
                'import-status-error',
              )
            : null}
          {createImport.isError
            ? errorText(describeError(createImport.error), 'import-error')
            : null}
          <View style={{ flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap' }}>
            <Button
              testID="import-button"
              label={createImport.isPending || active ? 'Importing…' : 'Import'}
              disabled={
                !preview.data.data.importable ||
                seat === null ||
                createImport.isPending ||
                (active && !job.isError)
              }
              onPress={() => seat !== null && createImport.mutate({ ...ref, seat })}
            />
            <Button
              testID="not-mine-button"
              variant="secondary"
              label="No, go back"
              onPress={() => {
                setRef(null);
                setSeat(null);
                createImport.reset();
              }}
            />
          </View>
        </Card>
      ) : null}
    </Screen>
  );
}
